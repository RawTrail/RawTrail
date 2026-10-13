//go:build integration

// The agent_sessions table (issue #1281): one row per agent session on a host, folded from every exec carrying the session marker.

package tests

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fleetdm/edr/server/detection/api"
	"github.com/fleetdm/edr/server/detection/internal/graph"
)

type sessionRow struct {
	SessionID string  `db:"agent_session_id"`
	Name      string  `db:"agent_name"`
	Version   *string `db:"agent_version"`
	AgentPID  *int    `db:"agent_pid"`
	CWD       *string `db:"cwd"`
	FirstNs   int64   `db:"first_seen_ns"`
	LastNs    int64   `db:"last_seen_ns"`
}

func dumpSessions(t *testing.T, db *sqlx.DB, host string) []sessionRow {
	t.Helper()
	var rows []sessionRow
	require.NoError(t, db.SelectContext(t.Context(), &rows, `SELECT agent_session_id, agent_name, agent_version, agent_pid, cwd,
		first_seen_ns, last_seen_ns FROM agent_sessions WHERE host_id = ? ORDER BY agent_session_id`, host))
	return rows
}

// toolCall is an exec of a Bash tool call's shell carrying the session markers, with the cwd it ran in and the agent pid it names.
func toolCall(id string, ts int64, pid int, session, cwd, agentPID string) api.Event {
	return lineageEvt(id, "exec", ts, `{"pid":`+strconv.Itoa(pid)+`,"ppid":80673,"path":"/bin/zsh","cwd":"`+cwd+`","env":{`+
		`"CLAUDE_CODE_SESSION_ID":"`+session+`","AI_AGENT":"claude-code_2-1-296_agent","CLAUDE_PID":"`+agentPID+`"}}`)
}

func sessionEvents() []api.Event {
	return []api.Event{
		toolCall("e-one-a", 2010, 80730, sessionOne, "/Users/v/project", "80673"),
		toolCall("e-one-b", 3010, 80838, sessionOne, "/Users/v/project/sub", "80673"),
		toolCall("e-two", 4010, 80900, sessionTwo, "/Users/v/other", "99999999999"),
		lineageEvt("e-helper", "exec", 1030, `{"pid":80693,"ppid":80673,"path":"/opt/rg","env":{"AI_AGENT":"claude-code_2-1-296_harness"}}`),
		toolCall("e-long", 5010, 80950, strings.Repeat("x", 300), "/tmp", "80673"),
	}
}

// spec:server-process-graph-builder/agent-session-table/a-session-row-spans-its-tool-calls
func TestAgentSessions_Table(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	b := graph.NewBuilder(store, discardLogger())

	t.Run("one row per session, from its earliest tool call", func(t *testing.T) {
		t.Parallel()
		host := "sessions-" + t.Name()
		require.NoError(t, b.ProcessBatch(t.Context(), rewriteHost(sessionEvents(), host)))
		assert.Equal(t, []sessionRow{
			{SessionID: sessionOne, Name: "claude-code", Version: new("2.1.296"), AgentPID: new(80673), CWD: new("/Users/v/project"),
				FirstNs: 2010, LastNs: 3010},
			{SessionID: sessionTwo, Name: "claude-code", Version: new("2.1.296"), CWD: new("/Users/v/other"), FirstNs: 4010, LastNs: 4010},
		}, dumpSessions(t, db, host), "a helper and an oversized marker add no session, and a pid too large for the column is left unset")
	})

	t.Run("batches arriving out of order keep the earliest tool call", func(t *testing.T) {
		t.Parallel()
		host := "sessions-" + t.Name()
		events := rewriteHost(sessionEvents(), host)
		require.NoError(t, b.ProcessBatch(t.Context(), events[1:2]))
		require.NoError(t, b.ProcessBatch(t.Context(), events[0:1]))
		got := dumpSessions(t, db, host)
		require.Len(t, got, 1)
		assert.Equal(t, sessionRow{SessionID: sessionOne, Name: "claude-code", Version: new("2.1.296"), AgentPID: new(80673),
			CWD: new("/Users/v/project"), FirstNs: 2010, LastNs: 3010}, got[0])
	})

	t.Run("tool calls at the same nanosecond resolve by event id whatever the arrival order", func(t *testing.T) {
		t.Parallel()
		first := toolCall("e-tie-a", 2010, 80730, sessionOne, "/Users/v/a", "80673")
		second := toolCall("e-tie-b", 2010, 80731, sessionOne, "/Users/v/b", "80673")
		for name, batches := range map[string][][]api.Event{
			"one batch, in order":   {{first, second}},
			"one batch, reversed":   {{second, first}},
			"two batches, in order": {{first}, {second}},
			"two batches, reversed": {{second}, {first}},
		} {
			host := "sessions-tie-" + name
			for _, batch := range batches {
				require.NoError(t, b.ProcessBatch(t.Context(), rewriteHost(batch, host)))
			}
			got := dumpSessions(t, db, host)
			require.Len(t, got, 1, name)
			assert.Equal(t, new("/Users/v/a"), got[0].CWD, name)
		}
	})

	t.Run("a replayed batch changes nothing", func(t *testing.T) {
		t.Parallel()
		host := "sessions-" + t.Name()
		events := rewriteHost(sessionEvents(), host)
		require.NoError(t, b.ProcessBatch(t.Context(), events))
		first := dumpSessions(t, db, host)
		require.NoError(t, b.ProcessBatch(t.Context(), events))
		assert.Equal(t, first, dumpSessions(t, db, host))
	})

	t.Run("a sighting whose host cannot be stored does not stall the batch", func(t *testing.T) {
		t.Parallel()
		host := "sessions-" + t.Name()
		poison := toolCall("e-poison", 2020, 80731, sessionTwo, "/tmp", "80673")
		poison.HostID = strings.Repeat("h", 300)
		require.NoError(t, b.ProcessBatch(t.Context(), append(rewriteHost(sessionEvents()[:1], host), poison)))
		require.Len(t, dumpSessions(t, db, host), 1, "the valid session is stored")
		var poisoned int
		require.NoError(t, db.GetContext(t.Context(), &poisoned, `SELECT COUNT(*) FROM agent_sessions WHERE host_id = ?`, poison.HostID))
		assert.Zero(t, poisoned)
	})

	t.Run("one batch matches one event per batch", func(t *testing.T) {
		t.Parallel()
		batched, perEvent := "sessions-batch-"+t.Name(), "sessions-ref-"+t.Name()
		require.NoError(t, b.ProcessBatch(t.Context(), rewriteHost(sessionEvents(), batched)))
		events := rewriteHost(sessionEvents(), perEvent)
		slices.SortStableFunc(events, func(x, y api.Event) int { return cmp.Compare(x.TimestampNs, y.TimestampNs) })
		for _, e := range events {
			require.NoError(t, b.ProcessBatch(t.Context(), []api.Event{e}))
		}
		assert.Equal(t, dumpSessions(t, db, perEvent), dumpSessions(t, db, batched))
	})
}
