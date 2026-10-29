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

func promptEvent(id, host, session, promptID, text string, atNs int64) visibilityapi.Event {
	return activityEvent(id, host, "agent_prompt", atNs, `{"agent_session_id":"`+session+`","prompt_id":"`+promptID+`","text":"`+text+
		`","length":`+strconv.Itoa(len(text))+`,"redacted":1,"source":"typed","uid":501}`)
}

// spec:server-rest-api/agent-session-prompts/a-session-s-prompts-page-in-the-order-they-were-typed
// spec:server-rest-api/agent-session-prompts/a-search-keeps-the-prompts-whose-text-holds-it
func TestEventArchive_AgentPrompts(t *testing.T) {
	t.Parallel()
	arch := openTestArchive(t)
	ctx := context.Background()
	base := time.Now().UnixNano()
	// Five of the session's prompts, two of them at one instant so the order falls through to the event id; and one of another
	// session on the host, and one of the same session id on another host, which are not the session's.
	events := []visibilityapi.Event{
		promptEvent("p-c", "h1", "s1", "turn-3", "Deploy the API", base+2),
		promptEvent("p-a", "h1", "s1", "turn-1", "fix the failing test", base),
		promptEvent("p-b", "h1", "s1", "turn-2", "now run the linter", base+1),
		promptEvent("p-e", "h1", "s1", "turn-5", "deploy again", base+3),
		promptEvent("p-d", "h1", "s1", "turn-4", "check the logs", base+3),
		promptEvent("p-x", "h1", "s2", "turn-x", "another session", base),
		promptEvent("p-y", "h2", "s1", "turn-y", "another host", base),
	}
	require.NoError(t, arch.Insert(ctx, events))
	filter := visibilityapi.AgentPromptFilter{HostID: "h1", SessionID: "s1", Limit: 2}

	var turns []string
	require.Eventually(t, func() bool {
		turns = nil
		filter.Cursor = ""
		for range 10 {
			page, err := arch.AgentPrompts(ctx, filter)
			if err != nil {
				return false
			}
			for _, p := range page.Prompts {
				turns = append(turns, p.PromptID)
			}
			if page.NextCursor == "" {
				break
			}
			filter.Cursor = page.NextCursor
		}
		return len(turns) == 5
	}, 5*time.Second, 100*time.Millisecond)
	assert.Equal(t, []string{"turn-1", "turn-2", "turn-3", "turn-4", "turn-5"}, turns, "oldest first, ties by event id")

	first, err := arch.AgentPrompts(ctx, visibilityapi.AgentPromptFilter{HostID: "h1", SessionID: "s1", Limit: 1})
	require.NoError(t, err)
	assert.Equal(t, visibilityapi.AgentPrompt{PromptID: "turn-1", Text: "fix the failing test", Length: 20, Redacted: 1, Source: "typed",
		AtNs: base, UID: 501}, first.Prompts[0])

	found, err := arch.AgentPrompts(ctx, visibilityapi.AgentPromptFilter{HostID: "h1", SessionID: "s1", Search: "DEPLOY", Limit: 10})
	require.NoError(t, err)
	require.Len(t, found.Prompts, 2)
	assert.Equal(t, []string{"turn-3", "turn-5"}, []string{found.Prompts[0].PromptID, found.Prompts[1].PromptID})
	assert.Empty(t, found.NextCursor)

	for _, bad := range []string{"!!", "e30"} { // not base64url, and {} with no event id
		_, err := arch.AgentPrompts(ctx, visibilityapi.AgentPromptFilter{HostID: "h1", SessionID: "s1", Cursor: bad, Limit: 2})
		assert.ErrorIs(t, err, visibilityapi.ErrInvalidPromptCursor, bad)
	}
}
