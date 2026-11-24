package receiver

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// burstConnector is a stub connector whose events are all waiting before the loop reads the first, as a burst of process activity
// leaves them in the receiver's channel.
func burstConnector(n int) *stubConnector {
	c := newStubConnector(stubScript{})
	c.events = make(chan Event, n)
	for i := range n {
		c.events <- Event{Data: fmt.Appendf(nil, `{"seq":%d}`, i)}
	}
	return c
}

// drainRecorder gathers what OnEvent is handed, as the agent does, and records each batch OnDrained is asked to write.
type drainRecorder struct {
	mu       sync.Mutex
	gathered int
	batches  []int
	// ctxErrs is the context error each OnDrained call was handed: a write cancelled by the stop would lose the batch.
	ctxErrs []error
}

func (d *drainRecorder) onEvent(context.Context, Event) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.gathered++
}

func (d *drainRecorder) onDrained(ctx context.Context) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ctxErrs = append(d.ctxErrs, ctx.Err())
	d.batches = append(d.batches, d.gathered)
	d.gathered = 0
}

func (d *drainRecorder) snapshot() (int, []int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.gathered, append([]int(nil), d.batches...)
}

// spec:agent-xpc-receiver/a-burst-is-written-in-batches/a-burst-of-events-is-written-in-batches
//
// A burst waiting in the channel is handed to OnDrained in batches of at most maxDrainBatch, with the last batch written as soon as
// the channel is empty (issue #1584), so a burst costs a commit per batch rather than one per event.
func TestLoop_WritesABurstInBatches(t *testing.T) {
	t.Parallel()
	const burst = 2*maxDrainBatch + 76
	conn := burstConnector(burst)
	rec := &drainRecorder{}
	loop := NewLoop(func() Connector { return conn }, LoopConfig{ServiceName: "sys", HeartbeatInterval: time.Hour, HeartbeatTimeout: time.Second},
		LoopHooks{OnEvent: rec.onEvent, OnDrained: rec.onDrained, Sleep: (&recordingSleep{}).Sleep}, discardLogger())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { loop.Run(ctx); close(done) }()

	require.Eventually(t, func() bool {
		_, batches := rec.snapshot()
		total := 0
		for _, b := range batches {
			total += b
		}
		return total == burst
	}, 2*time.Second, 5*time.Millisecond)
	_, batches := rec.snapshot()
	assert.Equal(t, []int{maxDrainBatch, maxDrainBatch, 76}, batches)
	cancel()
	<-done
}

// What OnEvent gathered is written when the connection ends mid-burst, so a stop or a reconnect loses none of it.
func TestLoop_WritesWhatItGatheredWhenTheConnectionEnds(t *testing.T) {
	t.Parallel()
	conn := burstConnector(100)
	rec := &drainRecorder{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	handled := 0
	loop := NewLoop(func() Connector { return conn }, LoopConfig{ServiceName: "sys", HeartbeatInterval: time.Hour, HeartbeatTimeout: time.Second},
		LoopHooks{
			OnEvent: func(ctx context.Context, evt Event) {
				rec.onEvent(ctx, evt)
				if handled++; handled == 10 {
					cancel() // stop with 90 events still waiting, so the channel is not empty and nothing has drained yet
				}
			},
			OnDrained: rec.onDrained,
			Sleep:     (&recordingSleep{}).Sleep,
		}, discardLogger())
	loop.Run(ctx)

	gathered, batches := rec.snapshot()
	assert.Zero(t, gathered, "nothing gathered is left unwritten")
	total := 0
	for _, b := range batches {
		total += b
	}
	assert.Equal(t, handled, total, "every event handed to OnEvent was written")
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, err := range rec.ctxErrs {
		assert.NoError(t, err, "the write is not cancelled by the stop")
	}
}

// A stop that lands as the channel empties still writes the batch, with a context the stop does not cancel.
func TestLoop_AStopAtTheEndOfABurstStillWritesIt(t *testing.T) {
	t.Parallel()
	const burst = 5
	conn := burstConnector(burst)
	rec := &drainRecorder{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	handled := 0
	loop := NewLoop(func() Connector { return conn }, LoopConfig{ServiceName: "sys", HeartbeatInterval: time.Hour, HeartbeatTimeout: time.Second},
		LoopHooks{
			OnEvent: func(ctx context.Context, evt Event) {
				rec.onEvent(ctx, evt)
				if handled++; handled == burst {
					cancel() // the last event: the channel is now empty, so OnDrained fires right after this returns
				}
			},
			OnDrained: rec.onDrained,
			Sleep:     (&recordingSleep{}).Sleep,
		}, discardLogger())
	loop.Run(ctx)

	_, batches := rec.snapshot()
	require.Equal(t, []int{burst}, batches, "the burst is written once, as the channel empties")
	rec.mu.Lock()
	defer rec.mu.Unlock()
	assert.Equal(t, []error{nil}, rec.ctxErrs, "with a context the stop did not cancel")
}
