//go:build integration

package tests

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// spec:server-availability/queue-acknowledgements-survive-lock-contention/an-ack-that-loses-a-deadlock-is-retried
//
// TestEventLog_AckAndNackRetryALostDeadlock is the named repro for issue #1522. The ack's UPDATE meets the prune sweep's DELETE on the
// queue's gap locks, and one unretried deadlock left the batch in flight for the whole claim lease: the host's processing paused for 5
// minutes and the batch was then processed twice. The test builds one such cycle for real: a concurrent transaction holds the batch's
// second row and then asks for its first while the ack holds the first and waits on the second. The contender is made the heavier of
// the two (InnoDB rolls back the transaction that changed fewer rows), so the ack is always the victim, and without the retry it
// returns error 1213.
func TestEventLog_AckAndNackRetryALostDeadlock(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		resolve   func(t *testing.T, log visibilityapi.EventLog, ids []string, stamp int64) error
		processed int
	}{
		{"ack", func(t *testing.T, log visibilityapi.EventLog, ids []string, stamp int64) error {
			held, err := log.Ack(t.Context(), ids, stamp)
			if err == nil && !held {
				return fmt.Errorf("the retried ack must still hold its claim")
			}
			return err
		}, 1},
		{"nack", func(t *testing.T, log visibilityapi.EventLog, ids []string, stamp int64) error {
			result, err := log.Nack(t.Context(), ids, stamp, nil)
			if err == nil && !result.Held {
				return fmt.Errorf("the retried nack must still hold its claim")
			}
			return err
		}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			log, db := newEventLogWithDB(t)
			// The ack's IN list is read in primary key order, so it reaches first-event's row before second-event's.
			require.NoError(t, log.Append(ctx, []visibilityapi.Event{ev("a-first-event", "h-1522", 1, "fork"),
				ev("b-second-event", "h-1522", 2, "exec")}))
			claimed, stamp, err := log.ClaimForHost(ctx, "h-1522", 2)
			require.NoError(t, err)
			require.Len(t, claimed, 2)

			var deadlocksBefore int64
			require.NoError(t, db.GetContext(ctx, &deadlocksBefore,
				`SELECT COUNT FROM information_schema.INNODB_METRICS WHERE NAME = 'lock_deadlocks'`))

			contender, err := db.BeginTxx(ctx, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = contender.Rollback() })
			// Weight: InnoDB picks the victim by rows changed, so these unrelated inserts make the contender the one it keeps.
			for i := range 50 {
				_, err := contender.ExecContext(ctx, `INSERT INTO event_queue (event_id, host_id, timestamp_ns, event_type, payload)
					VALUES (?, 'h-1522-weight', ?, 'exec', '{}')`, fmt.Sprintf("weight-%d", i), i)
				require.NoError(t, err)
			}
			// The contender changes a column neither resolution reads, so the retried one still matches its claim.
			_, err = contender.ExecContext(ctx, `UPDATE event_queue SET platform = 'contender' WHERE event_id = 'b-second-event'`)
			require.NoError(t, err)

			done := make(chan error, 1)
			go func() { done <- tc.resolve(t, log, []string{"a-first-event", "b-second-event"}, stamp) }()

			// Close the cycle only once the resolution holds the first row and is queued on the second.
			require.Eventually(t, func() bool {
				var waiting int
				err := db.GetContext(ctx, &waiting, `SELECT COUNT(*) FROM performance_schema.data_lock_waits w
					JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID = w.REQUESTING_ENGINE_LOCK_ID
					WHERE l.OBJECT_SCHEMA = DATABASE() AND l.OBJECT_NAME = 'event_queue'`)
				return err == nil && waiting > 0
			}, 10*time.Second, 10*time.Millisecond, "the %s must block on the contender's row", tc.name)

			_, err = contender.ExecContext(ctx, `UPDATE event_queue SET platform = 'contender' WHERE event_id = 'a-first-event'`)
			require.NoError(t, err, "the heavier contender must survive the deadlock; the %s is the victim", tc.name)
			require.NoError(t, contender.Commit())

			require.NoError(t, <-done, "a %s that loses a deadlock must be retried, not returned as error 1213", tc.name)

			var deadlocksAfter int64
			require.NoError(t, db.GetContext(ctx, &deadlocksAfter,
				`SELECT COUNT FROM information_schema.INNODB_METRICS WHERE NAME = 'lock_deadlocks'`))
			require.Greater(t, deadlocksAfter, deadlocksBefore, "precondition: the test must have produced a real deadlock")

			var states []int
			require.NoError(t, db.SelectContext(ctx, &states,
				`SELECT processed FROM event_queue WHERE host_id = 'h-1522' ORDER BY event_id`))
			assert.Equal(t, []int{tc.processed, tc.processed}, states, "the retried %s applies to the whole batch", tc.name)
		})
	}
}
