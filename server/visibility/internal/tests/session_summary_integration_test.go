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

// A session summary's activity (issue #1422) is the rollup's totals with the file events by operation: an agent's folded row weighs
// the operations it stands for, a watched-path event's operation is its type, and an event of another generation is not counted.
//
// spec:server-rest-api/agent-session-summary/a-session-is-totalled
func TestEventArchive_ActivityTotalsForGenerations(t *testing.T) {
	t.Parallel()
	arch := openTestArchive(t)
	ctx := context.Background()
	base := time.Now().UnixNano()
	at := func(s int64) int64 { return base + s*int64(time.Second) }
	ns := func(s int64) string { return strconv.FormatInt(at(s), 10) }

	require.NoError(t, arch.Insert(ctx, []visibilityapi.Event{
		activityEvent("a1", "h1", "file_activity", at(1), `{"pid":300,"pidversion":3,"operation":"modify","path":"/r/main.go",`+
			`"coalesced_count":3,"last_timestamp_ns":`+ns(2)+`}`),
		activityEvent("a2", "h1", "file_activity", at(3), `{"pid":300,"pidversion":3,"operation":"create","path":"/r/new.go"}`),
		activityEvent("a3", "h1", "file_activity", at(4), `{"pid":300,"pidversion":3,"operation":"delete","path":"/r/old.go"}`),
		activityEvent("a4", "h1", "open", at(5), `{"pid":301,"pidversion":1,"path":"/etc/sudoers","flags":1}`),
		activityEvent("n1", "h1", "network_connect", at(6), `{"pid":301,"pidversion":1,"protocol":"tcp","direction":"outbound",`+
			`"remote_address":"1.1.1.1","remote_port":443,"coalesced_count":2}`),
		activityEvent("d1", "h1", "dns_query", at(7), `{"pid":300,"pidversion":3,"query_name":"example.com","query_type":"A"}`),
		// Another generation's write is not the scope's.
		activityEvent("x1", "h1", "file_activity", at(8), `{"pid":300,"pidversion":9,"operation":"create","path":"/r/other.go"}`),
	}))

	filter := visibilityapi.ActivityFilter{HostID: "h1", Generations: []visibilityapi.ProcessGeneration{
		{PID: 300, PIDVersion: 3}, {PID: 301, PIDVersion: 1},
	}}
	var got visibilityapi.ActivitySummary
	require.Eventually(t, func() bool {
		var err error
		got, err = arch.ActivityTotalsForGenerations(ctx, filter)
		return err == nil && got.DNS.Events == 1
	}, 5*time.Second, 100*time.Millisecond)

	assert.Equal(t, visibilityapi.ActivitySummary{
		Files: visibilityapi.FileTotals{
			ClassTotals: visibilityapi.ClassTotals{Events: 6, Distinct: 4},
			ByOperation: map[string]int64{"modify": 3, "create": 1, "delete": 1, "open": 1},
		},
		Network: visibilityapi.ClassTotals{Events: 2, Distinct: 1},
		DNS:     visibilityapi.ClassTotals{Events: 1, Distinct: 1},
	}, got)

	rollup, err := arch.ActivityForGenerations(ctx, filter)
	require.NoError(t, err)
	assert.Equal(t, rollup.Files.Events, got.Files.Events, "the totals are the rollup's")
	assert.Equal(t, rollup.Network.Distinct, got.Network.Distinct, "the totals are the rollup's")

	t.Run("a scope with no generation totals nothing", func(t *testing.T) {
		t.Parallel()
		empty, err := arch.ActivityTotalsForGenerations(ctx, visibilityapi.ActivityFilter{HostID: "h1"})
		require.NoError(t, err)
		assert.Equal(t, visibilityapi.ActivitySummary{Files: visibilityapi.FileTotals{ByOperation: map[string]int64{}}}, empty)
	})
}

// A session's tool calls are counted by tool (issue #1422) as they are listed: each call once however often the agent reported it,
// named by its first recording, a copy of the parent's call left out, and only the session's on its host.
//
// spec:server-rest-api/agent-session-summary/a-session-is-totalled
func TestEventArchive_AgentToolCallCounts(t *testing.T) {
	t.Parallel()
	arch := openTestArchive(t)
	ctx := context.Background()
	base := time.Now().UnixNano()
	at := func(s int64) int64 { return base + s*int64(time.Second) }
	call := func(id, session, toolUseID, tool, extra string, ts int64) visibilityapi.Event {
		return activityEvent(id, "h1", "agent_tool_call", ts, `{"agent_session_id":"`+session+`","tool_use_id":"`+toolUseID+`",`+
			`"tool_name":"`+tool+`","uid":501`+extra+`}`)
	}
	require.NoError(t, arch.Insert(ctx, []visibilityapi.Event{
		call("p1", "parent", "toolu_p", "Bash", "", at(1)),
		// The fork copied the parent's call, then made its own.
		call("f1", "fork", "toolu_p", "Bash", `,"origin_session_id":"parent"`, at(1)),
		call("f2", "fork", "toolu_a", "Bash", "", at(2)),
		call("f3", "fork", "toolu_b", "Read", "", at(3)),
		call("f4", "fork", "toolu_c", "Read", "", at(4)),
		call("f5", "fork", "toolu_d", "Edit", "", at(5)),
		// toolu_a reported again after a restart, under a tool name a later edit of the transcript gave it: counted once, as Bash.
		call("f6", "fork", "toolu_a", "Write", "", at(9)),
		// Another session's call, and another host's for this session.
		call("o1", "other", "toolu_e", "Bash", "", at(6)),
		activityEvent("h2", "h2", "agent_tool_call", at(7), `{"agent_session_id":"fork","tool_use_id":"toolu_f","tool_name":"Bash"}`),
	}))

	filter := visibilityapi.AgentToolCallFilter{HostID: "h1", SessionID: "fork", Limit: 1}
	var got []visibilityapi.ToolCallCount
	require.Eventually(t, func() bool {
		var err error
		got, err = arch.AgentToolCallCounts(ctx, filter)
		return err == nil && len(got) == 3
	}, 5*time.Second, 100*time.Millisecond)
	assert.Equal(t, []visibilityapi.ToolCallCount{
		{ToolName: "Read", Count: 2}, {ToolName: "Bash", Count: 1}, {ToolName: "Edit", Count: 1},
	}, got, "most used first, then by name; the limit does not cap the counts")

	t.Run("a window", func(t *testing.T) {
		t.Parallel()
		windowed := filter
		windowed.FromNs, windowed.ToNs = at(3), at(4)
		counts, err := arch.AgentToolCallCounts(ctx, windowed)
		require.NoError(t, err)
		assert.Equal(t, []visibilityapi.ToolCallCount{{ToolName: "Read", Count: 2}}, counts)
	})
}
