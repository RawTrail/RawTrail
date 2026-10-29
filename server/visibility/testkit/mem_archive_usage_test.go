package testkit

import (
	"context"
	"encoding/json"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/visibility/api"
)

// TestMemArchive_AgentUsageTotals pins the fake to the ClickHouse read (TestEventArchive_AgentUsageTotals): each message once, the
// fork's copy of its parent's message left out, a /clear message kept, by model heaviest output first and by mode first seen first.
func TestMemArchive_AgentUsageTotals(t *testing.T) {
	t.Parallel()
	usage := func(id, session, messageID, model string, out, ts int64, extra map[string]any) api.Event {
		payload := map[string]any{"agent_session_id": session, "message_id": messageID, "model": model, "input_tokens": 1,
			"output_tokens": out, "uid": 501}
		maps.Copy(payload, extra)
		raw, err := json.Marshal(payload)
		require.NoError(t, err)
		return api.Event{EventID: id, HostID: "h1", TimestampNs: ts, IngestedAtNs: ts, EventType: "agent_usage", Payload: raw}
	}
	arch := NewMemArchive()
	require.NoError(t, arch.Insert(context.Background(), []api.Event{
		usage("u-parent", "parent", "msg_p", "opus", 1000, 1, nil),
		usage("u-1", "fork", "msg_1", "opus", 50, 2, map[string]any{"permission_mode": "auto"}),
		usage("u-1-again", "fork", "msg_1", "opus", 50, 9, map[string]any{"permission_mode": "auto"}),
		usage("u-2", "fork", "msg_2", "sonnet", 20, 3, map[string]any{"permission_mode": "plan"}),
		usage("u-copy", "fork", "msg_p", "opus", 1000, 4, map[string]any{"origin_session_id": "parent"}),
		usage("u-clear", "fork", "msg_4", "opus", 5, 5, map[string]any{"origin_session_id": "parent"}),
	}))
	got, err := arch.AgentUsageTotals(context.Background(), api.AgentToolCallFilter{HostID: "h1", SessionID: "fork"})
	require.NoError(t, err)
	assert.Equal(t, []api.ModelUsage{
		{Model: "opus", Messages: 2, Input: 2, Output: 55},
		{Model: "sonnet", Messages: 1, Input: 1, Output: 20},
	}, got.Models)
	assert.Equal(t, []api.ModeUsage{
		{Mode: "auto", Messages: 1, FirstNs: 2, LastNs: 2},
		{Mode: "plan", Messages: 1, FirstNs: 3, LastNs: 3},
		{Mode: "", Messages: 1, FirstNs: 5, LastNs: 5},
	}, got.Modes)
}
