package mysql_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/internal/mysql"
)

// TestBumpSnapshotLastSeenBatch_RetriesALostDeadlock is the named repro for issue #1243. The heartbeat bump runs at ingest,
// concurrently with other writes to the processes table, and under load it loses InnoDB deadlocks. The test builds one such cycle
// for real: a concurrent transaction holds the second row and then asks for the first while the bump holds the first and waits on
// the second. The concurrent transaction is made the heavier of the two
// (InnoDB rolls back the transaction that changed fewer rows), so the bump is always the victim, and without the retry it returns
// error 1213.
//
// spec:server-process-graph-builder/heartbeat-freshness-bumps-survive-lock-contention/a-bump-that-loses-a-deadlock-is-retried-and-applied
func TestBumpSnapshotLastSeenBatch_RetriesALostDeadlock(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := t.Context()
	db := s.DB()

	const host = "h-1243"
	const forkNs = int64(1_000_000_000)
	const heartbeatNs = forkNs + int64(time.Hour)
	// Inserted in PID order, so the bump's scan reaches firstPID's row before secondPID's whichever index it reads through.
	const firstPID, secondPID = 4100, 4200
	rowIDs := make(map[int]int64, 2)
	for _, pid := range []int{firstPID, secondPID} {
		lastSeen := forkNs
		id, err := s.InsertProcess(ctx, api.Process{
			HostID: host, PID: pid, Path: "/usr/libexec/snap", ForkTimeNs: forkNs, IsSnapshot: true, LastSeenNs: &lastSeen,
		})
		require.NoError(t, err)
		rowIDs[pid] = id
	}

	var deadlocksBefore int64
	require.NoError(t, db.GetContext(ctx, &deadlocksBefore,
		`SELECT COUNT FROM information_schema.INNODB_METRICS WHERE NAME = 'lock_deadlocks'`))

	contender, err := db.BeginTxx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = contender.Rollback() })
	// Weight: InnoDB picks the victim by rows changed, so these unrelated inserts make the contender the transaction it keeps.
	for i := range 50 {
		_, err := contender.ExecContext(ctx, `INSERT INTO hosts (host_id) VALUES (?)`, fmt.Sprintf("h-1243-weight-%d", i))
		require.NoError(t, err)
	}
	_, err = contender.ExecContext(ctx, `UPDATE processes SET path = '/contender' WHERE id = ?`, rowIDs[secondPID])
	require.NoError(t, err)

	bumpDone := make(chan error, 1)
	go func() {
		bumpDone <- s.BumpSnapshotLastSeenBatch(ctx, host, []mysql.SnapshotHeartbeat{
			{PID: firstPID, TimestampNs: heartbeatNs}, {PID: secondPID, TimestampNs: heartbeatNs},
		})
	}()

	// Close the cycle only once the bump holds the first row and is queued on the second. performance_schema, not INNODB_TRX: the
	// latter is a periodically refreshed cache and was observed to omit the waiting transaction entirely.
	require.Eventually(t, func() bool {
		var waiting int
		err := db.GetContext(ctx, &waiting, `SELECT COUNT(*) FROM performance_schema.data_lock_waits w
			JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID = w.REQUESTING_ENGINE_LOCK_ID
			WHERE l.OBJECT_SCHEMA = DATABASE() AND l.OBJECT_NAME = 'processes'`)
		return err == nil && waiting > 0
	}, 10*time.Second, 10*time.Millisecond, "the bump must block on the contender's row")

	_, err = contender.ExecContext(ctx, `UPDATE processes SET path = '/contender' WHERE id = ?`, rowIDs[firstPID])
	require.NoError(t, err, "the heavier contender must survive the deadlock; the bump is the victim")
	require.NoError(t, contender.Commit())

	require.NoError(t, <-bumpDone, "a bump that loses a deadlock must be retried, not returned as error 1213")

	var deadlocksAfter int64
	require.NoError(t, db.GetContext(ctx, &deadlocksAfter,
		`SELECT COUNT FROM information_schema.INNODB_METRICS WHERE NAME = 'lock_deadlocks'`))
	require.Greater(t, deadlocksAfter, deadlocksBefore, "precondition: the test must have produced a real deadlock")

	for _, pid := range []int{firstPID, secondPID} {
		var lastSeen int64
		require.NoError(t, db.GetContext(ctx, &lastSeen, `SELECT last_seen_ns FROM processes WHERE id = ?`, rowIDs[pid]))
		assert.Equal(t, heartbeatNs, lastSeen, "the retried bump must apply to pid %d", pid)
	}
}
