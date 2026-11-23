package pipeline

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/coordination/leader"
	rulesapi "github.com/rawtrail/rawtrail/server/rules/api"
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// The bound on settling a batch (issue #1571). The integration tests in server/detection/internal/tests show the statement lands
// after a stop; these pin how long it is allowed to take, on a fake clock so the grace is not waited out for real.
func TestSettleContext(t *testing.T) {
	t.Parallel()

	t.Run("outlives the stop for the grace and then ends", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			worker, stop := context.WithCancel(context.Background())
			settleCtx, settled := settleContext(worker)
			defer settled()

			stop()
			synctest.Wait()
			require.NoError(t, settleCtx.Err(), "the stop itself must not cancel the settling statement")

			time.Sleep(settleGrace - time.Nanosecond)
			synctest.Wait()
			require.NoError(t, settleCtx.Err(), "the statement has the whole grace to land")

			time.Sleep(time.Nanosecond)
			synctest.Wait()
			assert.ErrorIs(t, settleCtx.Err(), context.Canceled, "an unresponsive database cannot hold a stop open past the grace")
		})
	})

	t.Run("carries no deadline while the worker runs", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			settleCtx, settled := settleContext(context.Background())
			defer settled()

			_, hasDeadline := settleCtx.Deadline()
			assert.False(t, hasDeadline)
			time.Sleep(time.Hour)
			synctest.Wait()
			assert.NoError(t, settleCtx.Err(), "an acknowledgement waiting on the host lock in normal running is not timed out")
		})
	})

	t.Run("ends when the caller is done with it", func(t *testing.T) {
		t.Parallel()
		settleCtx, settled := settleContext(context.Background())
		settled()
		assert.ErrorIs(t, settleCtx.Err(), context.Canceled)
	})
}

// A requeue inside the host lock outlives a stop but not the loss of the lock itself, because a requeue without the lock reopens the
// ordering window the lock closes (issue #863).
func TestSettleUnderLock(t *testing.T) {
	t.Parallel()

	t.Run("a stop leaves the requeue its grace", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			worker, stop := context.WithCancel(context.Background())
			locked, unlock := context.WithCancel(worker)
			defer unlock()
			settleCtx, settled := settleUnderLock(worker, locked)
			defer settled()

			stop()
			synctest.Wait()
			assert.NoError(t, settleCtx.Err(), "the lock is still held after a stop, so the requeue may land")
		})
	})

	t.Run("a lost lock ends the requeue at once", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			locked, loseLock := context.WithCancelCause(context.Background())
			settleCtx, settled := settleUnderLock(context.Background(), locked)
			defer settled()

			loseLock(leader.ErrLockLost)
			synctest.Wait()
			assert.ErrorIs(t, settleCtx.Err(), context.Canceled, "a requeue without the lock must not run")
		})
	})

	// A stop that follows a lost lock cancels the worker too, so the worker's state cannot be what tells the two apart.
	t.Run("a lock lost before a stop still ends the requeue", func(t *testing.T) {
		t.Parallel()
		worker, stop := context.WithCancel(context.Background())
		locked, loseLock := context.WithCancelCause(worker)
		loseLock(leader.ErrLockLost)
		stop()

		settleCtx, settled := settleUnderLock(worker, locked)
		defer settled()

		assert.ErrorIs(t, settleCtx.Err(), context.Canceled, "the lock is gone, whatever the worker is doing")
	})
}

// liveContextRecorder records whether each monitor tally write was given a live context. The real recorder writes to MySQL, so a
// write on a cancelled context is a lost tally.
type liveContextRecorder struct {
	writes []error
}

func (r *liveContextRecorder) RecordMonitorMatches(ctx context.Context, _ rulesapi.MonitorTally) error {
	r.writes = append(r.writes, ctx.Err())
	return ctx.Err()
}

// stopDuringEvaluation stops the worker as its evaluation returns, which is how a stop reaches a batch whose rules all ran.
type stopDuringEvaluation struct {
	stop  context.CancelFunc
	tally rulesapi.MonitorTally
	err   error
}

func (e stopDuringEvaluation) Evaluate(context.Context, []visibilityapi.Event) (rulesapi.MonitorTally, error) {
	e.stop()
	return e.tally, e.err
}

// stopDuringFold stops the worker while its batch is folded, and fails the fold with it.
type stopDuringFold struct{ stop context.CancelFunc }

func (b stopDuringFold) ProcessBatch(ctx context.Context, _ []visibilityapi.Event) error {
	b.stop()
	return ctx.Err()
}

// spec:server-availability/a-graceful-stop-settles-the-batch-in-progress/a-settled-batch-s-monitor-counts-are-recorded
//
// A batch settled after a stop is never evaluated again, so its monitor tally has to be written on a context that outlives the
// stop too, or it is lost with it (issue #1571).
func TestStopRecordsTheSettledBatchTally(t *testing.T) {
	t.Parallel()

	tally := rulesapi.MonitorTally{{RuleID: "imported", HostID: "host-a", Severity: "high", Count: 2}}
	carried, err := encodeMonitorTally(tally)
	require.NoError(t, err)

	cases := []struct {
		name string
		// build returns the processor's stages, which stop the worker through stop.
		build func(stop context.CancelFunc) (batchBuilder, batchEvaluator)
		log   *scriptedEventLog
	}{
		{
			name: "an acknowledged batch",
			build: func(stop context.CancelFunc) (batchBuilder, batchEvaluator) {
				return stubBuilder{}, stopDuringEvaluation{stop: stop, tally: tally}
			},
			log: &scriptedEventLog{batch: oneEventBatch()},
		},
		{
			name: "a batch withdrawn after evaluation",
			build: func(stop context.CancelFunc) (batchBuilder, batchEvaluator) {
				return stubBuilder{}, stopDuringEvaluation{stop: stop, tally: tally, err: errors.New("evaluation stopped")}
			},
			log: &scriptedEventLog{batch: oneEventBatch(), setAside: 1},
		},
		{
			name: "a batch withdrawn at the fold",
			build: func(stop context.CancelFunc) (batchBuilder, batchEvaluator) {
				return stopDuringFold{stop: stop}, stubEvaluator{}
			},
			log: &scriptedEventLog{batch: oneEventBatch(), setAside: 1, carried: carried},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			worker, stop := context.WithCancel(t.Context())
			defer stop()
			builder, evaluator := tc.build(stop)
			rec := &liveContextRecorder{}
			p := newTestProcessor(t, tc.log, builder, evaluator, singleCycleOpts(&capturingLogHandler{}))
			p.SetMonitorMatchRecorder(rec)

			p.ProcessOnce(worker)

			require.Error(t, worker.Err(), "the worker must have been stopped mid-batch, or this proves nothing")
			assert.Equal(t, []error{nil}, rec.writes, "the settled batch's tally is written once, on a live context")
		})
	}
}

// interruptibleLockCoordinator runs the locked section under a context that loseLock cancels with leader.ErrLockLost, as the real
// coordinator does when the lock goes while the section runs. Unlike lockLosingCoordinator, the lock is held when the section starts.
type interruptibleLockCoordinator struct {
	stubCoordinator
	loseLock func()
}

func (c *interruptibleLockCoordinator) DoOnceIfLeader(ctx context.Context, _ string, fn func(context.Context) error) (bool, error) {
	locked, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	c.loseLock = func() { cancel(leader.ErrLockLost) }
	return true, fn(locked)
}

// failFoldWith fails the fold after cancelling the context it names, failing with that cancellation as a real fold's queries do.
type failFoldWith struct{ cancel func() }

func (b failFoldWith) ProcessBatch(ctx context.Context, _ []visibilityapi.Event) error {
	b.cancel()
	return ctx.Err()
}

// ctxObservingLog fails a Nack on a cancelled context, as the real queue's statement does, and records what each one was given.
type ctxObservingLog struct {
	*scriptedEventLog
	nackCtxErrs []error
}

func (l *ctxObservingLog) Nack(ctx context.Context, ids []string, stamp int64, tally []byte) (visibilityapi.NackResult, error) {
	l.nackCtxErrs = append(l.nackCtxErrs, ctx.Err())
	if err := ctx.Err(); err != nil {
		return visibilityapi.NackResult{}, err
	}
	return l.scriptedEventLog.Nack(ctx, ids, stamp, tally)
}

// The fold's requeue runs inside the host lock, so it is handed back after a stop, which leaves the lock held until the section
// returns, and not after the lock is lost, which would let a concurrent claimer fold this host's later events first (issue #863).
func TestFoldRequeueAfterAStopButNotAfterALostLock(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// interrupt is what the fold does to its own run: stop the worker, or lose the lock.
		interrupt   func(stopWorker context.CancelFunc, coord *interruptibleLockCoordinator) func()
		wantNacked  []string
		wantCtxErrs []error
	}{
		{
			name: "a stop hands the batch back",
			interrupt: func(stopWorker context.CancelFunc, _ *interruptibleLockCoordinator) func() {
				return stopWorker
			},
			wantNacked:  []string{"evt-1"},
			wantCtxErrs: []error{nil},
		},
		{
			// spec:server-availability/a-graceful-stop-settles-the-batch-in-progress/a-requeue-does-not-outlive-a-lost-host-lock
			name: "a lost lock leaves the batch to its lease",
			interrupt: func(_ context.CancelFunc, coord *interruptibleLockCoordinator) func() {
				return func() { coord.loseLock() }
			},
			wantNacked:  nil,
			wantCtxErrs: []error{context.Canceled},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			worker, stopWorker := context.WithCancel(t.Context())
			defer stopWorker()
			coord := &interruptibleLockCoordinator{}
			log := &ctxObservingLog{scriptedEventLog: &scriptedEventLog{batch: oneEventBatch()}}
			opts := singleCycleOpts(&capturingLogHandler{})
			opts.Coordinator = coord
			p := newTestProcessor(t, log, failFoldWith{cancel: tc.interrupt(stopWorker, coord)}, stubEvaluator{}, opts)

			p.ProcessOnce(worker)

			assert.Equal(t, tc.wantCtxErrs, log.nackCtxErrs)
			assert.Equal(t, tc.wantNacked, log.nacked)
		})
	}
}
