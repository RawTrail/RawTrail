// Package bootstrap wires the one-time daemon prelude that every fleet-edr Go
// binary needs: load config from env, install a signal-cancel context, initialise
// OTel, initialise the structured logger, and hand back a deferred flush.
//
// Each daemon's main() drops from ~40 lines of identical setup to a handful of
// calls, which also eliminates the primary source of cross-binary duplication
// that SonarQube was flagging on the refactor PR.
package bootstrap

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
	"uuid"

	"github.com/jmoiron/sqlx"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/fleetdm/edr/internal/logging"
	"github.com/fleetdm/edr/internal/observability"
	"github.com/fleetdm/edr/internal/observability/tracing"
	"github.com/fleetdm/edr/server/config"
)

// OTelFlushTimeout caps the OTel exporter flush so a dead collector cannot
// stall server shutdown.
//
// Exported because a deployment has to allow for it: it is the last stage of a graceful stop, after the drain, the in-flight
// wait and the loop joins, and a container killed before it loses the telemetry describing its own shutdown (issue #1127).
const OTelFlushTimeout = 5 * time.Second

// instanceID returns this process's OTel service.instance.id. It is generated once (sync.OnceValue) and stable for the process
// lifetime, so every span and metric the binary emits carries the same replica identifier. That is what lets an operator tell
// replicas apart in the backend behind a load balancer (server-availability: replica identity). A random UUID is used rather than
// the hostname so the identifier is unique even when two replicas share a hostname (e.g. host-network pods).
var instanceID = sync.OnceValue(func() string { return uuid.New().String() })

// Env is the bundle of ready-to-use primitives returned by Init.
//
// ctx is NOT a field on Env because Go convention says contexts flow through
// function calls, not across struct boundaries. Init returns it separately so
// the caller threads it explicitly.
type Env struct {
	// Cancel releases the signal.NotifyContext. Safe to call multiple times.
	Cancel context.CancelFunc
	// Config is the validated env configuration.
	Config *config.Config
	// Logger is installed as slog.Default() as a side effect of Init.
	Logger *slog.Logger
	// FlushOTel flushes buffered OTel telemetry. Callers defer this immediately
	// after Init so any later startup failure still gets drained to the collector.
	FlushOTel func()
}

// Options identify the daemon to observability (service.name / service.version).
type Options struct {
	ServiceName    string
	ServiceVersion string
	// Commit and BuildTime are logged on the start line by Start. They are build metadata set by -ldflags, not telemetry resources.
	Commit    string
	BuildTime string
	// TraceSampler is required: it becomes the TracerProvider's head sampler, so trace export volume is capped (issue #374). Init wraps
	// it in sdktrace.ParentBased, so a sampled parent forces its children sampled.
	TraceSampler *tracing.RouteTierSampler
}

// Init runs the daemon prelude. On any error the partial state is torn down before returning so the caller only has to check err.
// Returns ctx first so the caller's main() threads it through the rest of the program explicitly (rather than reaching it through
// env.Ctx, since a struct-stashed context is a Go anti-pattern). The caller is responsible for calling env.Cancel (on clean exit) and
// env.FlushOTel (on every exit path, ideally via defer immediately after Init returns).
func Init(opts Options) (context.Context, *Env, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	shutdownOTel, err := observability.Init(ctx, observability.OptionsFromEnv(observability.Options{
		ServiceName:       opts.ServiceName,
		ServiceVersion:    opts.ServiceVersion,
		ServiceInstanceID: instanceID(),
		Sampler:           sdktrace.ParentBased(opts.TraceSampler),
	}))
	if err != nil {
		cancel()
		return nil, nil, err
	}

	logger, err := logging.New(os.Stderr, logging.Options{
		Level:               cfg.LogLevel,
		Format:              cfg.LogFormat,
		InstrumentationName: opts.ServiceName,
	})
	if err != nil {
		// Best-effort OTel flush before we bail: the partial OTLP state would
		// otherwise leak a pending batch.
		flushWithTimeout(shutdownOTel)
		cancel()
		return nil, nil, err
	}
	slog.SetDefault(logger)

	flush := func() { flushWithTimeout(shutdownOTel) }
	return ctx, &Env{
		Cancel:    cancel,
		Config:    cfg,
		Logger:    logger,
		FlushOTel: flush,
	}, nil
}

// Start is the prelude both HTTP binaries share: Init, the start line, and the MySQL pool. The returned stop func cancels ctx,
// flushes OTel and closes the pool, in that order; the caller defers it as soon as Start returns a nil error. On error, whatever Start
// had already set up is torn down before it returns, so the caller only has to return the error.
func Start(opts Options) (context.Context, *Env, *sqlx.DB, func(), error) {
	ctx, env, err := Init(opts)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	db, stop, err := startAfterInit(ctx, env, opts)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return ctx, env, db, stop, nil
}

// startAfterInit is the part of Start that follows Init: the start line and the MySQL pool. It takes the Env Init built, so a test
// can drive it without loading config from the process environment. On an open failure it cancels ctx and flushes OTel itself.
func startAfterInit(ctx context.Context, env *Env, opts Options) (*sqlx.DB, func(), error) {
	env.Logger.InfoContext(ctx, opts.ServiceName+" starting",
		"addr", env.Config.ListenAddr,
		"version", opts.ServiceVersion,
		"commit", opts.Commit,
		"build_time", opts.BuildTime,
	)
	db, err := OpenDB(ctx, env.Config.DSN)
	if err != nil {
		env.Logger.ErrorContext(ctx, "open db", "err", err)
		env.Cancel()
		env.FlushOTel()
		return nil, nil, err
	}
	// The flush comes before the close: shutting OTel down collects every observable gauge one last time, and three of them count rows
	// in MySQL, so a closed pool would lose their final export and log a WARN for each (issue #1274). The cancel stays ahead of the flush so
	// what the shutdown logs is still exported; the gauges are collected under the flush's own context, not ctx.
	stop := func() {
		env.Cancel()
		env.FlushOTel()
		_ = db.Close()
	}
	return db, stop, nil
}

// Main runs a binary's run func and, if it fails, prints the error to stderr and exits with status 2.
func Main(run func() error) {
	if err := run(); err != nil {
		_, _ = os.Stderr.WriteString("fatal: " + err.Error() + "\n")
		os.Exit(2)
	}
}

// flushWithTimeout caps OTel flush at OTelFlushTimeout so a dead collector
// doesn't stall the shutdown path.
func flushWithTimeout(shutdown func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), OTelFlushTimeout)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		slog.Default().WarnContext(ctx, "otel shutdown", "err", err)
	}
}
