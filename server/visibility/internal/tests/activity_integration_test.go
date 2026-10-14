//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	visibilityapi "github.com/fleetdm/edr/server/visibility/api"
)

// activityEvent builds one archived event of the given type with the given payload, recent enough to survive the archive's TTL.
func activityEvent(id, host, etype string, tsNs int64, payload string) visibilityapi.Event {
	return visibilityapi.Event{
		EventID: id, HostID: host, TimestampNs: tsNs, IngestedAtNs: tsNs + 1, EventType: etype, Platform: "darwin",
		Payload: json.RawMessage(payload),
	}
}

// spec:server-rest-api/process-activity-rollup/a-scope-s-activity-is-totalled-and-listed-by-process
func TestEventArchive_ActivityForGenerations(t *testing.T) {
	t.Parallel()
	arch := openTestArchive(t)
	ctx := context.Background()
	base := time.Now().UnixNano()
	at := func(s int64) int64 { return base + s*int64(time.Second) }

	events := []visibilityapi.Event{
		// Generation (100, 1) opens /tmp/a twice and renames it; generation (200, 2) opens it once.
		activityEvent("f1", "h1", "open", at(1), `{"pid":100,"pidversion":1,"path":"/tmp/a","flags":1}`),
		activityEvent("f2", "h1", "open", at(2), `{"pid":100,"pidversion":1,"path":"/tmp/a","flags":1}`),
		activityEvent("f3", "h1", "file_rename", at(3), `{"pid":100,"pidversion":1,"source_path":"/tmp/a","path":"/tmp/b"}`),
		activityEvent("f4", "h1", "open", at(4), `{"pid":200,"pidversion":2,"path":"/tmp/a","flags":1}`),
		// A later process that reused pid 100, and a legacy event with no pidversion: neither belongs to the scope.
		activityEvent("f5", "h1", "open", at(5), `{"pid":100,"pidversion":9,"path":"/tmp/x","flags":1}`),
		activityEvent("f6", "h1", "open", at(6), `{"pid":100,"path":"/tmp/y","flags":1}`),
		// A coalesced flow stands for 3 connections, last seen at 60s; one more plain connection to the same destination.
		activityEvent("n1", "h1", "network_connect", at(10), `{"pid":100,"pidversion":1,"protocol":"tcp","direction":"outbound",`+
			`"remote_address":"1.1.1.1","remote_port":443,"coalesced_count":3,"last_timestamp_ns":`+strconv.FormatInt(at(60), 10)+`}`),
		activityEvent("n2", "h1", "network_connect", at(11), `{"pid":100,"pidversion":1,"protocol":"tcp","direction":"outbound",`+
			`"remote_address":"1.1.1.1","remote_port":443}`),
		activityEvent("n3", "h1", "network_connect", at(12), `{"pid":200,"pidversion":2,"protocol":"udp","direction":"outbound",`+
			`"remote_address":"8.8.8.8","remote_port":53,"remote_hostname":"dns.google"}`),
		// Two answers for one name; the same name on another host is outside the scope.
		activityEvent("d1", "h1", "dns_query", at(20), `{"pid":100,"pidversion":1,"query_name":"example.com","query_type":"A",`+
			`"response_addresses":["5.6.7.8","1.2.3.4"]}`),
		activityEvent("d2", "h1", "dns_query", at(21), `{"pid":100,"pidversion":1,"query_name":"example.com","query_type":"A",`+
			`"response_addresses":["1.2.3.4"]}`),
		activityEvent("d3", "h2", "dns_query", at(22), `{"pid":100,"pidversion":1,"query_name":"example.com","query_type":"A"}`),
	}
	require.NoError(t, arch.Insert(ctx, events))
	// A re-delivered event is counted once.
	require.NoError(t, arch.Insert(ctx, events[:1]))

	scope := []visibilityapi.ProcessGeneration{{PID: 100, PIDVersion: 1}, {PID: 200, PIDVersion: 2}}
	filter := visibilityapi.ActivityFilter{HostID: "h1", Generations: scope}
	var got visibilityapi.ActivityRollup
	require.Eventually(t, func() bool {
		var err error
		got, err = arch.ActivityForGenerations(ctx, filter)
		return err == nil && got.DNS.Events == 2
	}, 5*time.Second, 100*time.Millisecond)

	// Counted by hand from the events above.
	assert.Equal(t, visibilityapi.ActivityTotals{Events: 4, Distinct: 2}, got.Files.ActivityTotals, "/tmp/a and /tmp/b")
	assert.Equal(t, visibilityapi.ActivityTotals{Events: 5, Distinct: 2}, got.Network.ActivityTotals, "3 coalesced + 1 + 1")
	assert.Equal(t, visibilityapi.ActivityTotals{Events: 2, Distinct: 1}, got.DNS.ActivityTotals)

	row := func(pid, pv, count, first, last int64) visibilityapi.ActivityRow {
		return visibilityapi.ActivityRow{PID: pid, PIDVersion: pv, Count: count, FirstNs: first, LastNs: last}
	}
	assert.Equal(t, []visibilityapi.FileActivityRow{
		{ActivityRow: row(100, 1, 2, at(1), at(2)), Operation: "open", Path: "/tmp/a"},
		{ActivityRow: row(100, 1, 1, at(3), at(3)), Operation: "file_rename", Path: "/tmp/b", SourcePath: "/tmp/a"},
		{ActivityRow: row(200, 2, 1, at(4), at(4)), Operation: "open", Path: "/tmp/a"},
	}, got.Files.Rows)
	assert.Equal(t, []visibilityapi.NetworkActivityRow{
		{ActivityRow: row(100, 1, 4, at(10), at(60)), RemoteAddress: "1.1.1.1", RemotePort: 443, Protocol: "tcp"},
		{ActivityRow: row(200, 2, 1, at(12), at(12)), RemoteAddress: "8.8.8.8", RemotePort: 53, Protocol: "udp", RemoteHostname: "dns.google"},
	}, got.Network.Rows)
	assert.Equal(t, []visibilityapi.DNSActivityRow{
		{ActivityRow: row(100, 1, 2, at(20), at(21)), QueryName: "example.com", Addresses: []string{"1.2.3.4", "5.6.7.8"}},
	}, got.DNS.Rows)
	assert.False(t, got.Files.Truncated || got.Network.Truncated || got.DNS.Truncated)

	t.Run("each class reports a cut list", func(t *testing.T) {
		filter := filter
		filter.Limit = 1
		cut, err := arch.ActivityForGenerations(ctx, filter)
		require.NoError(t, err)
		assert.Len(t, cut.Files.Rows, 1)
		assert.True(t, cut.Files.Truncated)
		assert.True(t, cut.Network.Truncated)
		assert.False(t, cut.DNS.Truncated, "one name fits a limit of one")
		assert.Equal(t, int64(4), cut.Files.Events, "totals are the scope's, not the page's")
	})

	t.Run("no generations reads nothing", func(t *testing.T) {
		empty, err := arch.ActivityForGenerations(ctx, visibilityapi.ActivityFilter{HostID: "h1"})
		require.NoError(t, err)
		assert.Equal(t, visibilityapi.ActivityTotals{}, empty.Files.ActivityTotals)
		assert.NotNil(t, empty.Files.Rows, "an empty list marshals as []")
	})
}

// An AI agent's folded file activity (#1287) is read beside the watched-path events: its operation comes from the payload, a folded
// row weighs its coalesced_count and ends at its last_timestamp_ns, and a rolled-up row is listed as the directory it stands for.
//
// spec:server-rest-api/agent-file-activity-reads/agent-file-activity-is-totalled-by-the-operations-it-stands-for
func TestEventArchive_ActivityAgentFiles(t *testing.T) {
	t.Parallel()
	arch := openTestArchive(t)
	ctx := context.Background()
	base := time.Now().UnixNano()
	at := func(s int64) int64 { return base + s*int64(time.Second) }
	ns := func(s int64) string { return strconv.FormatInt(at(s), 10) }

	require.NoError(t, arch.Insert(ctx, []visibilityapi.Event{
		// One file changed three times in a window, then once more in a later one.
		activityEvent("a1", "h1", "file_activity", at(30), `{"pid":300,"pidversion":3,"operation":"modify","path":"/r/main.go",`+
			`"coalesced_count":3,"last_timestamp_ns":`+ns(31)+`}`),
		activityEvent("a2", "h1", "file_activity", at(40), `{"pid":300,"pidversion":3,"operation":"modify","path":"/r/main.go"}`),
		activityEvent("a3", "h1", "file_activity", at(41), `{"pid":300,"pidversion":3,"operation":"rename","path":"/r/b",`+
			`"source_path":"/r/a"}`),
		// An install folded into its directory.
		activityEvent("a4", "h1", "file_activity", at(50), `{"pid":300,"pidversion":3,"operation":"create","path":"/r/node_modules",`+
			`"coalesced_count":1856,"last_timestamp_ns":`+ns(51)+`,"rolled_up":true}`),
	}))

	filter := visibilityapi.ActivityFilter{HostID: "h1", Generations: []visibilityapi.ProcessGeneration{{PID: 300, PIDVersion: 3}}}
	var got visibilityapi.ActivityRollup
	require.Eventually(t, func() bool {
		var err error
		got, err = arch.ActivityForGenerations(ctx, filter)
		return err == nil && len(got.Files.Rows) == 3
	}, 5*time.Second, 100*time.Millisecond)

	assert.Equal(t, visibilityapi.ActivityTotals{Events: 1861, Distinct: 3}, got.Files.ActivityTotals, "3 + 1 + 1 + 1856 operations")
	row := func(count, first, last int64) visibilityapi.ActivityRow {
		return visibilityapi.ActivityRow{PID: 300, PIDVersion: 3, Count: count, FirstNs: first, LastNs: last}
	}
	assert.Equal(t, []visibilityapi.FileActivityRow{
		{ActivityRow: row(4, at(30), at(40)), Operation: "modify", Path: "/r/main.go"},
		{ActivityRow: row(1, at(41), at(41)), Operation: "rename", Path: "/r/b", SourcePath: "/r/a"},
		{ActivityRow: row(1856, at(50), at(51)), Operation: "create", Path: "/r/node_modules", RolledUp: true},
	}, got.Files.Rows)
}

// A session's tool calls are read from the archive (issue #1288): one per tool_use_id at its first recording, though the agent reports
// a call again after a restart, oldest first, and only the session's and the host's.
//
// spec:server-rest-api/agent-tool-calls-endpoint/a-session-s-tool-calls-are-listed-once-each
func TestEventArchive_AgentToolCalls(t *testing.T) {
	t.Parallel()
	arch := openTestArchive(t)
	ctx := context.Background()
	base := time.Now().UnixNano()
	at := func(s int64) int64 { return base + s*int64(time.Second) }
	call := func(id, session, toolUseID, extra string, ts int64) visibilityapi.Event {
		return activityEvent(id, "h1", "agent_tool_call", ts, `{"agent_session_id":"`+session+`","tool_use_id":"`+toolUseID+`",`+
			`"tool_name":"Bash"`+extra+`}`)
	}
	require.NoError(t, arch.Insert(ctx, []visibilityapi.Event{
		call("c1", "s1", "toolu_a", `,"command":"ls","description":"List files"`, at(1)),
		call("c2", "s1", "toolu_b", `,"command":"pwd","sidechain":true`, at(2)),
		// The agent restarted and reported toolu_a again, later.
		call("c3", "s1", "toolu_a", `,"command":"ls","description":"List files"`, at(9)),
		call("c4", "s1", "toolu_c", `,"command":"ls","truncated":true`, at(3)),
		// Another session's call and another host's are not this session's.
		call("c5", "s2", "toolu_d", `,"command":"ls"`, at(4)),
		activityEvent("c6", "h2", "agent_tool_call", at(5), `{"agent_session_id":"s1","tool_use_id":"toolu_e","tool_name":"Bash"}`),
	}))

	filter := visibilityapi.AgentToolCallFilter{HostID: "h1", SessionID: "s1", Limit: 10}
	var got []visibilityapi.AgentToolCall
	require.Eventually(t, func() bool {
		var err error
		got, _, err = arch.AgentToolCalls(ctx, filter)
		return err == nil && len(got) == 3
	}, 5*time.Second, 100*time.Millisecond)
	assert.Equal(t, []visibilityapi.AgentToolCall{
		{ToolUseID: "toolu_a", ToolName: "Bash", Command: "ls", Description: "List files", CalledAtNs: at(1)},
		{ToolUseID: "toolu_b", ToolName: "Bash", Command: "pwd", Sidechain: true, CalledAtNs: at(2)},
		{ToolUseID: "toolu_c", ToolName: "Bash", Command: "ls", Truncated: true, CalledAtNs: at(3)},
	}, got)

	t.Run("a window and a limit", func(t *testing.T) {
		t.Parallel()
		windowed := filter
		windowed.FromNs, windowed.ToNs, windowed.Limit = at(2), at(3), 1
		cut, truncated, err := arch.AgentToolCalls(ctx, windowed)
		require.NoError(t, err)
		assert.Equal(t, []string{"toolu_b"}, []string{cut[0].ToolUseID})
		assert.True(t, truncated, "toolu_c is in the window too")
	})
}
