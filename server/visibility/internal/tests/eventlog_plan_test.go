//go:build integration

package tests

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
	"github.com/rawtrail/rawtrail/server/visibility/internal/eventlog"
)

// seedBacklog inserts n never-claimed events for host in one statement, timestamps from 1, so the optimizer sees a backlog large
// enough that reading all of it is not the cheapest plan.
func seedBacklog(t *testing.T, db *sqlx.DB, host string, n int) {
	t.Helper()
	ctx := t.Context()
	_, err := db.ExecContext(ctx, "SET SESSION cte_max_recursion_depth = ?", n+1)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO event_queue (event_id, host_id, timestamp_ns, event_type, payload)
		WITH RECURSIVE seq (i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM seq WHERE i < ?)
		SELECT CONCAT(?, '-', i), ?, i, 'exec', '{}' FROM seq`, n, host, host)
	require.NoError(t, err)
}

// explainRows returns each EXPLAIN row of query as "key type extra" for the rows that read event_queue.
func explainRows(t *testing.T, db *sqlx.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := db.QueryxContext(t.Context(), "EXPLAIN "+query, args...)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var plans []string
	for rows.Next() {
		row := map[string]any{}
		require.NoError(t, rows.MapScan(row))
		if planCell(row["table"]) == "event_queue" {
			plans = append(plans, strings.Join([]string{planCell(row["key"]), planCell(row["type"]), planCell(row["Extra"])}, " | "))
		}
	}
	require.NoError(t, rows.Err())
	return plans
}

// spec:server-availability/queue-reads-cost-the-same-at-any-backlog-depth/a-deep-backlog-does-not-slow-the-next-host-or-the-next-claim
//
// TestEventLog_QueueReadsDoNotScanTheBacklog pins the plans of the processor's two hot reads, the host hint and the claim. A plan is
// the only place this shows: both return the same rows whether they read one index entry per host or the whole backlog, and only
// the cost differs. With 4.9M events queued for one host, the hint took 8 to 10 seconds and a claim 3 to 4, run by every worker on
// every tick, so the processor drained slower than one agent sent and the host's agent sessions fell days behind (issue #1515).
func TestEventLog_QueueReadsDoNotScanTheBacklog(t *testing.T) {
	t.Parallel()
	_, db := newEventLogWithDB(t)
	seedBacklog(t, db, "plan-big-host", 20000)
	for i := range 20 {
		seedBacklog(t, db, fmt.Sprintf("plan-host-%02d", i), 50)
	}
	// Refresh the statistics after the bulk seed, which the optimizer otherwise reads as an empty table.
	_, err := db.ExecContext(t.Context(), "ANALYZE TABLE event_queue")
	require.NoError(t, err)

	t.Run("host hint reads one entry per host for the never-claimed arm", func(t *testing.T) {
		plans := explainRows(t, db, eventlog.PendingHostsSQL, int64(1), int64(1), 16)
		assert.Contains(t, plans, "idx_event_queue_claim | range | Using where; Using index for group-by",
			"the never-claimed arm must be a loose index scan, plans: %v", plans)
	})
	t.Run("claim walks the host's events in order and stops at the limit", func(t *testing.T) {
		for name, query := range map[string]string{"never claimed": eventlog.ClaimNeverClaimedSQL, "expired": eventlog.ClaimExpiredSQL} {
			args := []any{"plan-big-host", 0, int64(0), 500}
			if name == "expired" {
				args = []any{"plan-big-host", int64(1), 0, int64(0), 500}
			}
			plans := explainRows(t, db, query, args...)
			require.Len(t, plans, 1, name)
			assert.True(t, strings.HasPrefix(plans[0], "idx_event_queue_claim | "), "%s reads idx_event_queue_claim, plan: %s", name, plans[0])
			assert.NotContains(t, plans[0], "filesort", "%s must not sort the host's backlog, plan: %s", name, plans[0])
		}
	})
}

// spec:server-availability/queue-reads-cost-the-same-at-any-backlog-depth/a-claim-takes-the-oldest-events-across-both-claim-states
//
// TestEventLog_ClaimMergesBothClaimStatesInOrder pins the merge of the claim's two reads: never-claimed and lease-expired events
// interleave in time, and a claim must hand back the oldest limit of both together, not all of one state before the other.
func TestEventLog_ClaimMergesBothClaimStatesInOrder(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	log, db := newEventLogWithDB(t)
	require.NoError(t, log.Append(ctx, []visibilityapi.Event{
		ev("t1", "h1", 1, "fork"), ev("t2", "h1", 2, "exec"), ev("t3", "h1", 3, "exec"),
		ev("t4", "h1", 4, "exec"), ev("t5", "h1", 5, "exec"),
	}))
	// Two earlier claims that each crashed: t2 and t4 sit in expired claims, the rest were never claimed.
	_, err := db.ExecContext(ctx, "UPDATE event_queue SET processed = 2, claimed_at_ns = 1 WHERE event_id IN ('t2', 't4')")
	require.NoError(t, err)

	claimed, _, err := log.ClaimForHost(ctx, "h1", 4)
	require.NoError(t, err)
	assert.Equal(t, []string{"t1", "t2", "t3", "t4"}, ids(claimed), "the oldest four across both claim states, in event order")

	rest, _, err := log.ClaimForHost(ctx, "h1", 4)
	require.NoError(t, err)
	assert.Empty(t, rest, "t5 waits behind the in-flight batch, which now holds the host's floor")
}

// planCell renders a MySQL EXPLAIN cell, which arrives as []byte or nil.
func planCell(v any) string {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
