package testkit

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/visibility/api"
)

// TestMemArchive_AgentToolCalls pins the fake to the ClickHouse read (TestEventArchive_AgentToolCalls): one call per tool_use_id at its
// first recording, oldest first, only the session's and the host's, cut at the limit.
func TestMemArchive_AgentToolCalls(t *testing.T) {
	t.Parallel()
	call := func(id, host, session, toolUseID string, ts int64) api.Event {
		payload, err := json.Marshal(map[string]any{"agent_session_id": session, "tool_use_id": toolUseID, "tool_name": "Bash",
			"command": "ls", "uid": 501})
		require.NoError(t, err)
		return api.Event{EventID: id, HostID: host, TimestampNs: ts, IngestedAtNs: ts, EventType: "agent_tool_call", Payload: payload}
	}
	arch := NewMemArchive()
	require.NoError(t, arch.Insert(context.Background(), []api.Event{
		call("c1", "h1", "s1", "toolu_a", 10),
		call("c2", "h1", "s1", "toolu_b", 20),
		call("c3", "h1", "s1", "toolu_a", 90), // reported again after a restart
		call("c4", "h1", "s2", "toolu_c", 30),
		call("c5", "h2", "s1", "toolu_d", 40),
	}))

	got, truncated, err := arch.AgentToolCalls(context.Background(), api.AgentToolCallFilter{HostID: "h1", SessionID: "s1", Limit: 10})
	require.NoError(t, err)
	assert.False(t, truncated)
	assert.Equal(t, []api.AgentToolCall{
		{ToolUseID: "toolu_a", ToolName: "Bash", Command: "ls", CalledAtNs: 10, UID: 501},
		{ToolUseID: "toolu_b", ToolName: "Bash", Command: "ls", CalledAtNs: 20, UID: 501},
	}, got)

	cut, truncated, err := arch.AgentToolCalls(context.Background(),
		api.AgentToolCallFilter{HostID: "h1", SessionID: "s1", FromNs: 15, ToNs: 95, Limit: 1})
	require.NoError(t, err)
	assert.True(t, truncated, "toolu_a's second recording is in the window too")
	assert.Equal(t, "toolu_b", cut[0].ToolUseID)
}
