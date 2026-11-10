package mysql_test

import (
	"context"
	"regexp"
	"strconv"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/internal/mysql"
)

// seedSession records an agent session on its own host with n processes stamped with it.
func seedSession(t *testing.T, ctx context.Context, db *sqlx.DB, hostID string, n int) {
	t.Helper()
	seedProcesses(t, ctx, db, hostID, n, 1_000_000)
	_, err := db.ExecContext(ctx, "UPDATE processes SET agent_session_id = 'session', uid = 501 WHERE host_id = ?", hostID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO agent_sessions (host_id, agent_session_id, agent_name, agent_version, agent_pid, cwd,
		first_seen_ns, first_event_id, last_seen_ns) VALUES (?, 'session', 'claude-code', '2', 1, '/', 1, 'e', 2)`, hostID)
	require.NoError(t, err)
}

// spec:server-rest-api/agent-sessions-list-cost-per-page/a-session-s-process-count-stops-at-its-bound
//
// TestListAgentSessions_CapsProcessCountAtItsBound pins what a row says on either side of the bound. A session one past the bound
// reports the bound and says it is capped; one exactly at the bound is exact.
func TestListAgentSessions_CapsProcessCountAtItsBound(t *testing.T) {
	t.Parallel()
	store, db := newStoreAndDB(t)
	ctx := t.Context()
	bound := mysql.SessionListProcessCountBound
	seedSession(t, ctx, db, "count-over-host", bound+1)
	seedSession(t, ctx, db, "count-at-host", bound)
	seedSession(t, ctx, db, "count-small-host", 3)

	result, err := store.ListAgentSessions(ctx, api.AgentSessionFilter{}, "", 10)
	require.NoError(t, err)
	type count struct {
		N      int64
		Capped bool
	}
	got := map[string]count{}
	for _, r := range result.Rows {
		got[r.HostID] = count{r.ProcessCount, r.ProcessCountCapped}
	}
	assert.Equal(t, map[string]count{
		"count-over-host":  {int64(bound), true},
		"count-at-host":    {int64(bound), false},
		"count-small-host": {3, false},
	}, got)
}

// spec:server-rest-api/agent-sessions-list-cost-per-page/a-session-s-process-count-stops-at-its-bound
//
// TestSessionProcessCounts_ReadsAtMostTheBound pins the cost the bound exists for, which the counts above cannot show: past the bound
// a count that read the whole session reports the same capped value. EXPLAIN ANALYZE reports how many index entries the read took.
// On dogfood an unbounded count over one page read 2.2M entries and took 9.5 s (issue #1547).
func TestSessionProcessCounts_ReadsAtMostTheBound(t *testing.T) {
	t.Parallel()
	_, db := newStoreAndDB(t)
	ctx := t.Context()
	bound := mysql.SessionListProcessCountBound
	seedSession(t, ctx, db, "count-cost-host", bound+500)

	var plan string
	require.NoError(t, db.GetContext(ctx, &plan, "EXPLAIN ANALYZE "+mysql.SessionProcessCountsSQL(1),
		0, "count-cost-host", "session", bound+1))
	lookups := regexp.MustCompile(`(?i)index (?:lookup|range scan) on processes[^\n]*actual time=\S+ rows=(\d+)`).FindAllStringSubmatch(plan, -1)
	require.NotEmpty(t, lookups, "the count must read the session index, plan:\n%s", plan)
	for _, lookup := range lookups {
		read, err := strconv.Atoi(lookup[1])
		require.NoError(t, err)
		assert.LessOrEqual(t, read, bound+1, "a count reads at most the bound and one more, plan:\n%s", plan)
	}
}
