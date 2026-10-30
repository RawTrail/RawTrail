//go:build integration

// The agent sessions that ran with permissions bypassed (issue #1427): recorded from agent_usage events, and filtered on in the
// sessions list.

package tests

import (
	"cmp"
	"slices"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/internal/graph"
)

const forkOfSessionOne = "3e8f4a21-7b6c-4d5e-9f01-a2b3c4d5e6f7"

type bypassRow struct {
	SessionID string `db:"agent_session_id"`
	FirstNs   int64  `db:"first_seen_ns"`
}

func dumpBypass(t *testing.T, db *sqlx.DB, host string) []bypassRow {
	t.Helper()
	var rows []bypassRow
	require.NoError(t, db.SelectContext(t.Context(), &rows,
		`SELECT agent_session_id, first_seen_ns FROM agent_session_bypass WHERE host_id = ? ORDER BY agent_session_id`, host))
	return rows
}

// usage is an agent_usage event of session in mode; extra carries any further payload members, such as a copy's origin.
func usage(id string, ts int64, session, mode, extra string) api.Event {
	modeMember := ""
	if mode != "" {
		modeMember = `,"permission_mode":"` + mode + `"`
	}
	return lineageEvt(id, "agent_usage", ts, `{"agent_session_id":"`+session+`","message_id":"msg_`+id+`","model":"claude-opus-5-5",`+
		`"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"thinking_tokens":0,"uid":501`+
		modeMember+extra+`}`)
}

func bypassEvents() []api.Event {
	return []api.Event{
		usage("u-one-auto", 1000, sessionOne, "auto", ""),
		usage("u-one-bypass-late", 3000, sessionOne, "bypassPermissions", ""),
		usage("u-one-bypass", 2000, sessionOne, "bypassPermissions", ""),
		usage("u-two-default", 2500, sessionTwo, "default", ""),
		usage("u-two-unnamed", 2600, sessionTwo, "", ""),
		// A forked session's copy of its parent's bypassed line names the fork, with the parent as its origin.
		usage("u-fork-copy", 4000, forkOfSessionOne, "bypassPermissions", `,"origin_session_id":"`+sessionOne+`"`),
	}
}

// spec:server-process-graph-builder/agent-session-bypass-record/a-session-that-ran-bypassed-is-recorded
func TestAgentSessions_BypassRecord(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	b := graph.NewBuilder(store, discardLogger())
	want := []bypassRow{{SessionID: sessionOne, FirstNs: 2000}, {SessionID: forkOfSessionOne, FirstNs: 4000}}
	slices.SortFunc(want, func(a, b bypassRow) int { return strings.Compare(a.SessionID, b.SessionID) })

	t.Run("one batch records each bypassed session at its earliest bypassed message", func(t *testing.T) {
		t.Parallel()
		host := "bypass-" + t.Name()
		require.NoError(t, b.ProcessBatch(t.Context(), rewriteHost(bypassEvents(), host)))
		assert.Equal(t, want, dumpBypass(t, db, host), "the fork's copy counts, and a session never bypassed is not recorded")
	})

	t.Run("one event per batch, oldest first, then all again, leaves the same record", func(t *testing.T) {
		t.Parallel()
		host := "bypass-" + t.Name()
		events := rewriteHost(bypassEvents(), host)
		slices.SortFunc(events, func(a, b api.Event) int { return cmp.Compare(a.TimestampNs, b.TimestampNs) })
		for _, evt := range events {
			require.NoError(t, b.ProcessBatch(t.Context(), []api.Event{evt}))
		}
		assert.Equal(t, want, dumpBypass(t, db, host), "a later bypassed message does not move the record later")
		require.NoError(t, b.ProcessBatch(t.Context(), events))
		assert.Equal(t, want, dumpBypass(t, db, host), "a replayed batch changes nothing")
	})

	t.Run("a session id too long to store is dropped and the rest of the batch kept", func(t *testing.T) {
		t.Parallel()
		host := "bypass-" + t.Name()
		events := append(rewriteHost(bypassEvents(), host), usage("u-long", 5000, strings.Repeat("x", 300), "bypassPermissions", ""))
		events[len(events)-1].HostID = host
		require.NoError(t, b.ProcessBatch(t.Context(), events))
		assert.Equal(t, want, dumpBypass(t, db, host))
	})
}

// spec:server-rest-api/agent-sessions-list-bypass-filter/the-list-keeps-the-sessions-that-ran-bypassed
func TestAgentSessionsList_BypassFilter(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	host := "bypass-list"
	seedSessionRow(t, db, host, sessionOne, "claude-code", "/Users/v/project", 100, 900)
	seedSessionRow(t, db, host, sessionTwo, "claude-code", "/Users/v/other", 200, 800)
	seedSessionRow(t, db, host, forkOfSessionOne, "claude-code", "/Users/v/project", 300, 700)
	require.NoError(t, graph.NewBuilder(store, discardLogger()).ProcessBatch(t.Context(), rewriteHost(bypassEvents(), host)))

	all, err := store.ListAgentSessions(t.Context(), api.AgentSessionFilter{HostID: host}, "", 10)
	require.NoError(t, err)
	bypassed := map[string]bool{}
	for _, r := range all.Rows {
		bypassed[r.AgentSessionID] = r.Bypass
	}
	assert.Equal(t, map[string]bool{sessionOne: true, sessionTwo: false, forkOfSessionOne: true}, bypassed)

	only, err := store.ListAgentSessions(t.Context(), api.AgentSessionFilter{HostID: host, Bypass: true}, "", 1)
	require.NoError(t, err)
	assert.Equal(t, int64(2), only.TotalMatched, "the total counts the filtered sessions")
	assert.Equal(t, []string{host + "/" + sessionOne}, sessionIDs(only.Rows))
	next, err := store.ListAgentSessions(t.Context(), api.AgentSessionFilter{HostID: host, Bypass: true}, only.NextCursor, 1)
	require.NoError(t, err)
	assert.Equal(t, []string{host + "/" + forkOfSessionOne}, sessionIDs(next.Rows), "the filter holds across pages")
	assert.Empty(t, next.NextCursor)
}
