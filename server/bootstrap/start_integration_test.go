//go:build integration

package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"os"
	"slices"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/config"
)

// TestStartAfterInit drives the part of the startup prelude that follows Init. The stop func must release everything it set up, in
// order: cancel the context, flush OTel, close the pool. The flush must find the pool open, because shutting OTel down collects the
// gauges that query MySQL (issue #1274). A failed MySQL open must cancel and flush itself, because the caller only returns the error.
func TestStartAfterInit(t *testing.T) {
	t.Parallel()
	dsn := os.Getenv("EDR_TEST_DSN") //nolint:forbidigo // approved test-DB boundary; see issue #172
	if dsn == "" {
		t.Skip("EDR_TEST_DSN not set")
	}
	// teardown records each teardown step, so a test can assert what ran and in what order. The pool has no hook of its own, so the
	// cancel and flush steps each record "close" first when the pool they were given is already closed by then.
	type teardown struct {
		steps []string
		pool  *sqlx.DB
	}
	// newEnv builds the Env Init would, wired to record into a fresh teardown.
	newEnv := func(dsn string) (context.Context, *Env, *teardown) {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		rec := &teardown{}
		record := func(step string) {
			closed := slices.Contains(rec.steps, "close")
			if !closed && rec.pool != nil && rec.pool.PingContext(context.Background()) != nil {
				rec.steps = append(rec.steps, "close")
			}
			rec.steps = append(rec.steps, step)
		}
		return ctx, &Env{
			Cancel: func() {
				record("cancel")
				cancel()
			},
			Config:    &config.Config{DSN: dsn},
			Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
			FlushOTel: func() { record("flush") },
		}, rec
	}
	opts := Options{ServiceName: "fleet-edr-start-test"}

	// spec:observability-instrumentation/shutdown-exports-the-final-gauge-values/the-final-collection-reaches-the-data-store
	t.Run("stop cancels and flushes, then closes the pool", func(t *testing.T) {
		t.Parallel()
		ctx, env, rec := newEnv(dsn)
		db, stop, err := startAfterInit(ctx, env, opts)
		require.NoError(t, err)
		require.NoError(t, db.PingContext(ctx))
		rec.pool = db
		assert.Empty(t, rec.steps, "nothing is torn down until stop runs")

		stop()

		assert.Equal(t, []string{"cancel", "flush"}, rec.steps, "the flush must run while the pool is still open")
		require.Error(t, ctx.Err(), "stop must cancel the context")
		assert.ErrorContains(t, db.PingContext(context.Background()), "database is closed", "stop must close the pool")
	})

	t.Run("an unreachable database cancels and flushes before returning", func(t *testing.T) {
		t.Parallel()
		ctx, env, rec := newEnv("root@tcp(127.0.0.1:1)/edr_test?parseTime=true")
		db, stop, err := startAfterInit(ctx, env, opts)
		require.ErrorContains(t, err, "ping db")
		assert.Nil(t, db)
		assert.Nil(t, stop)
		assert.Equal(t, []string{"cancel", "flush"}, rec.steps)
	})
}
