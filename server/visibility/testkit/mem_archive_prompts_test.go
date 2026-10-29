package testkit

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/visibility/api"
)

// TestMemArchive_AgentPrompts pins the fake to the ClickHouse read (TestEventArchive_AgentPrompts): the session's prompts only, oldest
// first with ties by event id, paged by the same cursor, kept by a search of their text regardless of case.
func TestMemArchive_AgentPrompts(t *testing.T) {
	t.Parallel()
	prompt := func(id, host, session, promptID, text string, ts int64) api.Event {
		payload, err := json.Marshal(map[string]any{"agent_session_id": session, "prompt_id": promptID, "text": text, "length": len(text),
			"uid": 501})
		require.NoError(t, err)
		return api.Event{EventID: id, HostID: host, TimestampNs: ts, IngestedAtNs: ts, EventType: "agent_prompt", Payload: payload}
	}
	arch := NewMemArchive()
	ctx := context.Background()
	require.NoError(t, arch.Insert(ctx, []api.Event{
		prompt("p-b", "h1", "s1", "turn-2", "Deploy it", 20),
		prompt("p-a", "h1", "s1", "turn-1", "fix the test", 10),
		prompt("p-d", "h1", "s1", "turn-4", "deploy again", 30),
		prompt("p-c", "h1", "s1", "turn-3", "check logs", 30),
		prompt("p-x", "h1", "s2", "turn-x", "another session", 10),
		prompt("p-y", "h2", "s1", "turn-y", "another host", 10),
	}))

	var turns []string
	filter := api.AgentPromptFilter{HostID: "h1", SessionID: "s1", Limit: 3}
	for range 3 {
		page, err := arch.AgentPrompts(ctx, filter)
		require.NoError(t, err)
		for _, p := range page.Prompts {
			turns = append(turns, p.PromptID)
		}
		if page.NextCursor == "" {
			break
		}
		filter.Cursor = page.NextCursor
	}
	assert.Equal(t, []string{"turn-1", "turn-2", "turn-3", "turn-4"}, turns)

	found, err := arch.AgentPrompts(ctx, api.AgentPromptFilter{HostID: "h1", SessionID: "s1", Search: "DEPLOY", Limit: 10})
	require.NoError(t, err)
	require.Len(t, found.Prompts, 2)
	assert.Equal(t, api.AgentPrompt{PromptID: "turn-2", Text: "Deploy it", Length: 9, AtNs: 20, UID: 501}, found.Prompts[0])

	_, err = arch.AgentPrompts(ctx, api.AgentPromptFilter{HostID: "h1", SessionID: "s1", Cursor: "!!", Limit: 2})
	assert.ErrorIs(t, err, api.ErrInvalidPromptCursor)
}
