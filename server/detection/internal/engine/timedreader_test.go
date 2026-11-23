package engine

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

// readDelay is how long every read of sleepingGraphReader takes, on synctest's clock.
const readDelay = time.Millisecond

// sleepingGraphReader takes readDelay over every read, standing in for a datastore with measurable latency.
type sleepingGraphReader struct{ stubGraphReader }

func (r sleepingGraphReader) GetProcessByPID(ctx context.Context, h string, pid int, at int64) (*api.Process, error) {
	time.Sleep(readDelay)
	return r.stubGraphReader.GetProcessByPID(ctx, h, pid, at)
}

func (r sleepingGraphReader) GetProcessByPIDVersion(ctx context.Context, h string, pid int, v uint32, at int64) (*api.Process, error) {
	time.Sleep(readDelay)
	return r.stubGraphReader.GetProcessByPIDVersion(ctx, h, pid, v, at)
}

func (r sleepingGraphReader) GetChildProcesses(ctx context.Context, h string, ppid int, tr api.TimeRange) ([]api.Process, error) {
	time.Sleep(readDelay)
	return r.stubGraphReader.GetChildProcesses(ctx, h, ppid, tr)
}

func (r sleepingGraphReader) GetExecChain(ctx context.Context, current api.Process) ([]api.Process, error) {
	time.Sleep(readDelay)
	return r.stubGraphReader.GetExecChain(ctx, current)
}

func (r sleepingGraphReader) GetNetworkEventsForProcess(ctx context.Context, h string, pid int, tr api.TimeRange) ([]api.Event, error) {
	time.Sleep(readDelay)
	return r.stubGraphReader.GetNetworkEventsForProcess(ctx, h, pid, tr)
}

func (r sleepingGraphReader) GetAgentSessionLedBy(ctx context.Context, agent api.Process, at int64) (string, error) {
	time.Sleep(readDelay)
	return r.stubGraphReader.GetAgentSessionLedBy(ctx, agent, at)
}

func (r sleepingGraphReader) GetHostEventsByType(ctx context.Context, h, eventType string, tr api.TimeRange) ([]api.Event, error) {
	time.Sleep(readDelay)
	return r.stubGraphReader.GetHostEventsByType(ctx, h, eventType, tr)
}

// Every read the timed reader passes through is charged to the evaluation that waited on it, so a rule is never billed against its
// evaluation budget for time the datastore took. graphReads lists each read a rule can make, so a new read that skipped the timing
// fails here.
func TestTimedReader_ChargesEveryReadToTheEvaluation(t *testing.T) {
	t.Parallel()
	for name, read := range graphReads() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				acc := &waitAccumulator{}
				ctx := withWaitAccumulator(t.Context(), acc)

				require.NoError(t, read(ctx, &timedReader{inner: sleepingGraphReader{}}))

				assert.Equal(t, int64(readDelay), acc.ns.Load(), "the read's whole time is added to the evaluation's wait")
			})
		})
	}
}
