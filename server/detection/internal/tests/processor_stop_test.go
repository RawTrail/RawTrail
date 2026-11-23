//go:build integration

// Coverage for issue #1571: a graceful stop must settle the batch the processor holds. Before the fix, the stop cancelled the
// worker's context, the batch failed, and the requeue ran on that same cancelled context, so it never reached MySQL. The rows stayed
// claimed (processed = 2) with no owner, and because a claim stops at its host's oldest in-flight event, the host's whole backlog
// waited out the 5-minute claim lease.
//
// Each test stops a real processor, holding a real advisory lock against the test MySQL, at the stage it names. The stages are held
// open by stand-ins that wait for the stop and then fail with it, which is what the real builder and engine do when a stop lands on
// one of their queries.

package tests

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/coordination/leader"
	"github.com/rawtrail/rawtrail/server/detection/internal/pipeline"
	rulesapi "github.com/rawtrail/rawtrail/server/rules/api"
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// stopBatchSize is smaller than the stream the tests seed, so the stopped batch has more of its host's events queued behind it. Those
// are the events a batch left claimed would hold up.
const stopBatchSize = 4

// heldUntilStop holds a pipeline stage open until the processor is stopped. entered closes when the stage is first reached, so the
// test stops the processor while a batch is known to be claimed rather than racing the claim.
type heldUntilStop struct {
	entered chan struct{}
	once    sync.Once
	// failOnStop is whether the stage fails once the stop arrives. An evaluation that does not fail is one that completed as the
	// stop arrived, whose batch must then be acknowledged.
	failOnStop bool
}

func newHeldUntilStop(failOnStop bool) *heldUntilStop {
	return &heldUntilStop{entered: make(chan struct{}), failOnStop: failOnStop}
}

func (h *heldUntilStop) wait(ctx context.Context) error {
	h.once.Do(func() { close(h.entered) })
	<-ctx.Done()
	if h.failOnStop {
		return ctx.Err()
	}
	return nil
}

// ProcessBatch stands in for the graph builder.
func (h *heldUntilStop) ProcessBatch(ctx context.Context, _ []visibilityapi.Event) error {
	return h.wait(ctx)
}

// Evaluate stands in for the detection engine.
func (h *heldUntilStop) Evaluate(ctx context.Context, _ []visibilityapi.Event) (rulesapi.MonitorTally, error) {
	return nil, h.wait(ctx)
}

// batchFolder and batchEvaluator are the processor's stage interfaces, which the pipeline package does not export.
type (
	batchFolder interface {
		ProcessBatch(context.Context, []visibilityapi.Event) error
	}
	batchEvaluator interface {
		Evaluate(context.Context, []visibilityapi.Event) (rulesapi.MonitorTally, error)
	}
)

// queueStates counts one host's queue rows by state.
type queueStates struct {
	pending, acked, claimed int
}

func hostQueueStates(t *testing.T, rig *queueRig, host string) queueStates {
	t.Helper()
	var states queueStates
	require.NoError(t, rig.db.QueryRowContext(t.Context(), `
		SELECT COALESCE(SUM(processed = 0), 0), COALESCE(SUM(processed = 1), 0), COALESCE(SUM(processed = 2), 0)
		FROM event_queue WHERE host_id = ?`, host).Scan(&states.pending, &states.acked, &states.claimed))
	return states
}

// stopMidBatch seeds host's stream, runs a processor with the given builder and evaluator until stage is reached, stops it, and waits
// for it to return. It returns the number of events seeded.
func stopMidBatch(
	t *testing.T, rig *queueRig, host string, builder batchFolder, evaluator batchEvaluator, stage *heldUntilStop,
) int {
	t.Helper()
	events := orderSensitiveEvents(host, time.Now().UnixNano(), 601, 602)
	require.Greater(t, len(events), stopBatchSize, "the stopped batch must have events queued behind it")
	require.NoError(t, rig.eventLog.Append(t.Context(), events))

	proc, err := pipeline.NewProcessor(rig.eventLog, builder, evaluator, pipeline.ProcessorOptions{
		Logger:      discardLogger(),
		Interval:    2 * time.Millisecond,
		Batch:       stopBatchSize,
		Concurrency: 1,
		Coordinator: leader.NewMySQL(rig.db, discardLogger()),
		ConnBudget:  rig.db.Stats().MaxOpenConnections,
	})
	require.NoError(t, err)
	runCtx, stop := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		_ = proc.Run(runCtx)
		close(done)
	}()

	select {
	case <-stage.entered:
	case <-time.After(30 * time.Second):
		stop()
		t.Fatalf("the processor never reached the stage under test")
	}
	require.Equal(t, stopBatchSize, hostQueueStates(t, rig, host).claimed, "a batch must be claimed when the stop arrives")
	stop()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("processor did not stop within 10s of cancel")
	}
	return len(events)
}

func TestProcessor_StopSettlesTheBatchInProgress(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// stageAt builds the processor's builder and evaluator around the held stage, given the rig's real builder.
		stageAt    func(rig *queueRig, stage *heldUntilStop) (batchFolder, batchEvaluator)
		failOnStop bool
		// wantAcked is whether the stopped batch is acknowledged rather than handed back.
		wantAcked bool
	}{
		{
			// spec:server-availability/a-graceful-stop-settles-the-batch-in-progress/a-batch-stopped-while-its-graph-is-built-is-handed-back
			name: "stopped while its graph is built",
			stageAt: func(_ *queueRig, stage *heldUntilStop) (batchFolder, batchEvaluator) {
				return stage, nil
			},
			failOnStop: true,
		},
		{
			// spec:server-availability/a-graceful-stop-settles-the-batch-in-progress/a-batch-stopped-during-evaluation-is-handed-back
			name: "stopped during evaluation",
			stageAt: func(rig *queueRig, stage *heldUntilStop) (batchFolder, batchEvaluator) {
				return rig.builder, stage
			},
			failOnStop: true,
		},
		{
			// spec:server-availability/a-graceful-stop-settles-the-batch-in-progress/a-batch-evaluated-as-the-stop-arrives-is-acknowledged
			name: "evaluated as the stop arrives",
			stageAt: func(rig *queueRig, stage *heldUntilStop) (batchFolder, batchEvaluator) {
				return rig.builder, stage
			},
			wantAcked: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rig := newQueueRig(t)
			// The host id carries the test name: advisory locks are server-scoped, so a parallel sibling must not share one.
			host := fmt.Sprintf("stop-%s", t.Name())
			stage := newHeldUntilStop(tc.failOnStop)
			builder, evaluator := tc.stageAt(rig, stage)

			seeded := stopMidBatch(t, rig, host, builder, evaluator, stage)

			states := hostQueueStates(t, rig, host)
			assert.Zero(t, states.claimed, "a graceful stop must not leave the batch claimed for its lease")
			if tc.wantAcked {
				assert.Equal(t, stopBatchSize, states.acked, "the evaluated batch is acknowledged")
				assert.Equal(t, seeded-stopBatchSize, states.pending, "the rest of the host's stream is still queued")
			} else {
				assert.Zero(t, states.acked, "a batch that failed is not acknowledged")
				assert.Equal(t, seeded, states.pending, "the batch is handed back alongside the rest of the host's stream")
			}
		})
	}
}

// spec:server-availability/a-graceful-stop-settles-the-batch-in-progress/the-backlog-resumes-after-a-restart-without-waiting-for-the-lease
//
// The consequence the operator sees. drainQueue gives the restarted processor 30 s, a tenth of the claim lease, so a batch left
// claimed by the stop fails this test rather than draining after the lease expires.
func TestProcessor_BacklogResumesAfterAStopWithoutWaitingForTheLease(t *testing.T) {
	t.Parallel()
	rig := newQueueRig(t)
	host := fmt.Sprintf("stop-resume-%s", t.Name())
	stage := newHeldUntilStop(true)

	seeded := stopMidBatch(t, rig, host, rig.builder, stage, stage)

	drainQueue(t, rig, 1, stopBatchSize, leader.NewMySQL(rig.db, discardLogger()))
	assert.Equal(t, queueStates{acked: seeded}, hostQueueStates(t, rig, host), "every event of the host's stream is processed")
}
