//go:build integration

// The AI agent sessions list (issue #1283): agent_sessions rows with their process and alert counts, filtered and keyset-paged.

package tests

import (
	"strconv"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fleetdm/edr/server/detection/api"
	"github.com/fleetdm/edr/server/detection/internal/mysql"
)

// seedSessionRow inserts one agent_sessions row as the graph builder's upsert would leave it.
func seedSessionRow(t *testing.T, db *sqlx.DB, host, session, agent, cwd string, firstNs, lastNs int64) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `INSERT INTO agent_sessions
		(host_id, agent_session_id, agent_name, agent_version, agent_pid, cwd, first_seen_ns, first_event_id, last_seen_ns)
		VALUES (?, ?, ?, '2.1.296', 80673, ?, ?, ?, ?)`, host, session, agent, cwd, firstNs, "ev-"+session, lastNs)
	require.NoError(t, err)
}

// seedSessionProcesses inserts n processes stamped with the session and returns their row ids.
func seedSessionProcesses(t *testing.T, store *mysql.Store, host, session string, n int) []int64 {
	t.Helper()
	var ids []int64
	for i := range n {
		id, err := store.InsertProcess(t.Context(), api.Process{HostID: host, PID: 1000 + i, PPID: 1, Path: "/bin/zsh",
			ForkTimeNs: int64(1000 + i), AgentSession: api.AgentSession{AgentSessionID: &session}})
		require.NoError(t, err)
		ids = append(ids, id)
	}
	return ids
}

func sessionIDs(rows []api.AgentSessionSummary) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.HostID+"/"+r.AgentSessionID)
	}
	return out
}

// spec:server-rest-api/agent-sessions-list/sessions-are-listed-with-what-they-did
func TestAgentSessionsList_RowsAndCounts(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	seedSessionRow(t, db, "host-a", "s-old", "claude-code", "/Users/v/old", 100, 200)
	seedSessionRow(t, db, "host-a", "s-new", "claude-code", "/Users/v/project", 300, 900)
	seedSessionRow(t, db, "host-b", "s-new", "codex", "/srv/app", 400, 500)
	procs := seedSessionProcesses(t, store, "host-a", "s-new", 3)
	seedSessionProcesses(t, store, "host-b", "s-new", 1)
	for i, disposition := range []api.AlertDisposition{api.AlertDispositionAlert, api.AlertDispositionAlert, api.AlertDispositionMonitor} {
		_, _, err := store.InsertAlert(t.Context(), api.Alert{HostID: "host-a", RuleID: "r" + strconv.Itoa(i), Source: api.AlertSourceDetection,
			Severity: api.SeverityHigh, Title: "t", Description: "d", ProcessID: procs[i], Subject: "proc:" + strconv.Itoa(i),
			Disposition: disposition}, nil)
		require.NoError(t, err)
	}

	res, err := store.ListAgentSessions(t.Context(), api.AgentSessionFilter{}, "", 50)
	require.NoError(t, err)
	require.Equal(t, []string{"host-a/s-new", "host-b/s-new", "host-a/s-old"}, sessionIDs(res.Rows), "most recently active first")
	assert.Equal(t, int64(3), res.TotalMatched)
	assert.Empty(t, res.NextCursor)
	top := res.Rows[0]
	assert.Equal(t, "claude-code", top.AgentName)
	assert.Equal(t, new("2.1.296"), top.AgentVersion)
	assert.Equal(t, new(80673), top.AgentPID)
	assert.Equal(t, new("/Users/v/project"), top.CWD)
	assert.Equal(t, [2]int64{300, 900}, [2]int64{top.FirstSeenNs, top.LastSeenNs})
	assert.Equal(t, int64(3), top.ProcessCount)
	assert.Equal(t, int64(2), top.AlertCount, "the monitor record is not an alert")
	assert.Equal(t, int64(1), res.Rows[1].ProcessCount, "the same session id on another host counts its own processes")
	assert.Zero(t, res.Rows[1].AlertCount)
	assert.Zero(t, res.Rows[2].ProcessCount)
}

// spec:server-rest-api/agent-sessions-list/filters-narrow-the-list
func TestAgentSessionsList_Filters(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	seedSessionRow(t, db, "host-a", "s1", "claude-code", "/a", 100, 200)
	seedSessionRow(t, db, "host-a", "s2", "codex", "/b", 300, 400)
	seedSessionRow(t, db, "host-b", "s3", "claude-code", "/c", 500, 600)
	for _, tc := range []struct {
		name   string
		filter api.AgentSessionFilter
		want   []string
	}{
		{"by host", api.AgentSessionFilter{HostID: "host-a"}, []string{"host-a/s2", "host-a/s1"}},
		{"by agent", api.AgentSessionFilter{AgentName: "claude-code"}, []string{"host-b/s3", "host-a/s1"}},
		{"active in a window", api.AgentSessionFilter{FromNs: 350, ToNs: 550}, []string{"host-b/s3", "host-a/s2"}},
		{"active since", api.AgentSessionFilter{FromNs: 450}, []string{"host-b/s3"}},
		{"active until", api.AgentSessionFilter{ToNs: 250}, []string{"host-a/s1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := store.ListAgentSessions(t.Context(), tc.filter, "", 50)
			require.NoError(t, err)
			assert.Equal(t, tc.want, sessionIDs(res.Rows))
			assert.Equal(t, int64(len(tc.want)), res.TotalMatched)
		})
	}
}

// spec:server-rest-api/agent-sessions-list/the-list-pages-by-cursor
func TestAgentSessionsList_Paging(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	var want []string
	for i := range 7 {
		// Two sessions share each last-seen time, so the host and id break the tie.
		session := "s:" + strconv.Itoa(i)
		seedSessionRow(t, db, "host-a", session, "claude-code", "/p", 100, int64(1000-(i/2)*10))
		want = append(want, "host-a/"+session)
	}

	var got []string
	cursor := ""
	for page := 0; ; page++ {
		res, err := store.ListAgentSessions(t.Context(), api.AgentSessionFilter{}, cursor, 3)
		require.NoError(t, err)
		assert.Equal(t, int64(7), res.TotalMatched, "the total is the filter's, not the page's")
		got = append(got, sessionIDs(res.Rows)...)
		if page == 0 {
			// A session becoming active between pages moves ahead of the cursor; it must not come back on a later page.
			_, err := db.ExecContext(t.Context(), `UPDATE agent_sessions SET last_seen_ns = 5000 WHERE agent_session_id = 's:5'`)
			require.NoError(t, err)
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	assert.Len(t, got, len(want)-1, "the session that moved ahead of the cursor is not seen again")
	seen := map[string]bool{}
	for _, id := range got {
		assert.False(t, seen[id], "returned twice: %s", id)
		seen[id] = true
	}

	_, err := store.ListAgentSessions(t.Context(), api.AgentSessionFilter{}, "not a cursor", 3)
	assert.ErrorIs(t, err, api.ErrInvalidCursor)
}
