package pipeline

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
