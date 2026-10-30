//go:build integration

package tests

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// spec:server-rest-api/agent-prompt-search/a-search-finds-prompts-across-sessions-newest-first
// spec:server-rest-api/a-prompt-search-is-bounded/a-search-is-bounded
func TestEventArchive_SearchAgentPrompts(t *testing.T) {
	t.Parallel()
	arch := openTestArchive(t)
	ctx := context.Background()
	base := time.Now().UnixNano()
	require.NoError(t, arch.Insert(ctx, []visibilityapi.Event{
		promptEvent("old", "h1", "s1", "p-old", "Deploy the old build", base-10_000),
		promptEvent("one", "h1", "s1", "p-one", "deploy to prod please", base+100),
		promptEvent("two", "h2", "s2", "p-two", "Can you DEPLOY it", base+200),
		promptEvent("three", "h1", "s3", "p-three", "redeploy staging", base+300),
		promptEvent("other", "h1", "s1", "p-other", "run the tests", base+400),
		promptEvent("literal", "h1", "s1", "p-literal", "100% done_ok", base+500),
		promptEvent("wildcard", "h1", "s1", "p-wildcard", "100 x done ok", base+600),
		// A tool call whose command holds the query is not a prompt.
		activityEvent("call", "h1", "agent_tool_call", base+700,
			`{"agent_session_id":"s1","tool_use_id":"t","tool_name":"Bash","command":"deploy","uid":501}`),
	}))
	search := func(s visibilityapi.AgentPromptSearch) visibilityapi.FoundAgentPromptPage {
		t.Helper()
		page, err := arch.SearchAgentPrompts(ctx, s)
		require.NoError(t, err)
		return page
	}
	ids := func(page visibilityapi.FoundAgentPromptPage) []string {
		var out []string
		for _, p := range page.Prompts {
			out = append(out, p.HostID+"/"+p.SessionID+"/"+p.PromptID)
		}
		return out
	}
	window := visibilityapi.AgentPromptSearch{Query: "deploy", FromNs: base, ToNs: base + 1_000, Limit: 10}
	require.Eventually(t, func() bool { return len(search(window).Prompts) == 3 }, 5*time.Second, 100*time.Millisecond)

	t.Run("across hosts and sessions, regardless of case, newest first, within the window", func(t *testing.T) {
		t.Parallel()
		page := search(window)
		assert.Equal(t, []string{"h1/s3/p-three", "h2/s2/p-two", "h1/s1/p-one"}, ids(page), "the prompt before the window is not read")
		assert.Equal(t, "deploy to prod please", page.Prompts[2].Text)
		assert.Equal(t, base+100, page.Prompts[2].AtNs)
		assert.Empty(t, page.NextCursor)
	})

	t.Run("one host", func(t *testing.T) {
		t.Parallel()
		oneHost := window
		oneHost.HostID = "h2"
		assert.Equal(t, []string{"h2/s2/p-two"}, ids(search(oneHost)))
	})

	t.Run("a page at a time by the cursor", func(t *testing.T) {
		t.Parallel()
		first := window
		first.Limit = 2
		page := search(first)
		assert.Equal(t, []string{"h1/s3/p-three", "h2/s2/p-two"}, ids(page), "the limit caps a page")
		require.NotEmpty(t, page.NextCursor)
		next := first
		next.Cursor = page.NextCursor
		rest := search(next)
		assert.Equal(t, []string{"h1/s1/p-one"}, ids(rest))
		assert.Empty(t, rest.NextCursor)
	})

	t.Run("percent and underscore are matched as themselves", func(t *testing.T) {
		t.Parallel()
		literal := window
		literal.Query = "100% done_ok"
		assert.Equal(t, []string{"h1/s1/p-literal"}, ids(search(literal)))
	})

	t.Run("a cursor that does not decode", func(t *testing.T) {
		t.Parallel()
		bad := window
		bad.Cursor = "not-a-cursor"
		_, err := arch.SearchAgentPrompts(ctx, bad)
		assert.ErrorIs(t, err, visibilityapi.ErrInvalidPromptCursor)
	})
}

// A prompt the agent sent again with changed text is matched by its newest text only, however the two versions sit in parts: the
// search reads under FINAL with no skip index, which could pass over the newer version and leave the older one standing.
//
// spec:server-rest-api/agent-prompt-search/a-search-finds-prompts-across-sessions-newest-first
func TestEventArchive_SearchAgentPromptsMatchesARedeliveredPromptByItsNewText(t *testing.T) {
	t.Parallel()
	arch := openTestArchive(t)
	ctx := context.Background()
	base := time.Now().UnixNano()
	// Two inserts, so the versions land in separate parts.
	require.NoError(t, arch.Insert(ctx, []visibilityapi.Event{promptEvent("same", "h1", "s1", "p-1", "rotate the zanzibar keys", base)}))
	resent := promptEvent("same", "h1", "s1", "p-1", "rotate the [REDACTED] keys", base)
	resent.IngestedAtNs++
	require.NoError(t, arch.Insert(ctx, []visibilityapi.Event{resent}))
	window := func(q string) visibilityapi.AgentPromptSearch {
		return visibilityapi.AgentPromptSearch{Query: q, FromNs: base - 1, ToNs: base + 1, Limit: 10}
	}
	require.Eventually(t, func() bool {
		page, err := arch.SearchAgentPrompts(ctx, window("redacted"))
		return err == nil && len(page.Prompts) == 1
	}, 5*time.Second, 100*time.Millisecond, "the new text is found")
	page, err := arch.SearchAgentPrompts(ctx, window("zanzibar"))
	require.NoError(t, err)
	assert.Empty(t, page.Prompts, "the old text is not")
}

// A search reads the window's agent_prompt rows, which the primary key narrows the read to, not the host's other events.
//
// spec:server-rest-api/a-prompt-search-is-bounded/a-search-is-bounded
func TestEventArchive_SearchAgentPromptsReadsOnlyPrompts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	arch, chDB := openTestArchiveWithHandle(t)

	const events = 60_000 // several 8192-row granules of execs beside a few prompts
	base := time.Now().UnixNano()
	batch := make([]visibilityapi.Event, 0, events+1)
	for i := range events {
		batch = append(batch, archiveEvent("ev-"+strconv.Itoa(i), "h1", "exec", base+int64(i), i))
	}
	batch = append(batch, promptEvent("needle", "h1", "s1", "p-needle", "rotate the zanzibar credentials", base+31337))
	require.NoError(t, arch.Insert(ctx, batch))

	search := visibilityapi.AgentPromptSearch{Query: "zanzibar", FromNs: base, ToNs: base + events, Limit: 10}
	require.Eventually(t, func() bool {
		page, err := arch.SearchAgentPrompts(ctx, search)
		return err == nil && len(page.Prompts) == 1
	}, 10*time.Second, 100*time.Millisecond, "the search finds the one prompt")

	_, err := chDB.ExecContext(ctx, "SYSTEM FLUSH LOGS")
	require.NoError(t, err)
	var readRows uint64
	require.NoError(t, chDB.QueryRowContext(ctx, `
		SELECT read_rows FROM system.query_log
		WHERE type = 'QueryFinish' AND current_database = currentDatabase() AND query LIKE '%prompt_text LIKE%'
		ORDER BY event_time_microseconds DESC LIMIT 1`).Scan(&readRows))
	assert.Less(t, readRows, uint64(events/2), "the read passes over the granules of other event types")
}
