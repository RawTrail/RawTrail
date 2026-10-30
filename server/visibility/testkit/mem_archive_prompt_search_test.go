package testkit

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/visibility/api"
)

// TestMemArchive_SearchAgentPrompts pins the fake to the ClickHouse search (TestEventArchive_SearchAgentPrompts): across hosts,
// regardless of case, within the window, of one host when asked, newest first, a page at a time.
func TestMemArchive_SearchAgentPrompts(t *testing.T) {
	t.Parallel()
	prompt := func(id, host, text string, ts int64) api.Event {
		raw, err := json.Marshal(map[string]any{"agent_session_id": "s-" + host, "prompt_id": "p-" + id, "text": text, "uid": 501})
		require.NoError(t, err)
		return api.Event{EventID: id, HostID: host, TimestampNs: ts, IngestedAtNs: ts, EventType: "agent_prompt", Payload: raw}
	}
	arch := NewMemArchive()
	ctx := context.Background()
	require.NoError(t, arch.Insert(ctx, []api.Event{
		prompt("old", "h1", "deploy it", 1),
		prompt("one", "h1", "Deploy to prod", 10),
		prompt("two", "h2", "please DEPLOY", 20),
		prompt("other", "h1", "run the tests", 30),
	}))
	ids := func(page api.FoundAgentPromptPage) []string {
		var out []string
		for _, p := range page.Prompts {
			out = append(out, p.HostID+"/"+p.SessionID+"/"+p.PromptID)
		}
		return out
	}
	search := api.AgentPromptSearch{Query: "deploy", FromNs: 5, ToNs: 100, Limit: 1}
	page, err := arch.SearchAgentPrompts(ctx, search)
	require.NoError(t, err)
	assert.Equal(t, []string{"h2/s-h2/p-two"}, ids(page))
	require.NotEmpty(t, page.NextCursor)
	search.Cursor = page.NextCursor
	rest, err := arch.SearchAgentPrompts(ctx, search)
	require.NoError(t, err)
	assert.Equal(t, []string{"h1/s-h1/p-one"}, ids(rest), "the prompt before the window is not found")
	assert.Empty(t, rest.NextCursor)

	oneHost, err := arch.SearchAgentPrompts(ctx, api.AgentPromptSearch{Query: "deploy", HostID: "h1", FromNs: 0, ToNs: 100, Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, []string{"h1/s-h1/p-one", "h1/s-h1/p-old"}, ids(oneHost))

	_, err = arch.SearchAgentPrompts(ctx, api.AgentPromptSearch{Query: "deploy", ToNs: 100, Cursor: "bad", Limit: 10})
	assert.ErrorIs(t, err, api.ErrInvalidPromptCursor)
}
