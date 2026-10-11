// fleet-edr-ingest is a standalone event ingestion service. It accepts events
// from agents and writes them to the ClickHouse archive and the MySQL event
// queue without any processing. A separate fleet-edr-server instance claims
// queued events and builds process graphs.
//
// It reads the server's storage, TLS, auth, proxy and shutdown-drain env vars;
// processor knobs are ignored.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jmoiron/sqlx"

	"github.com/fleetdm/edr/internal/keyring"
	"github.com/fleetdm/edr/internal/observability/tracing"
	"github.com/fleetdm/edr/server/bootstrap"
	"github.com/fleetdm/edr/server/config"
	detectionapi "github.com/fleetdm/edr/server/detection/api"
	detectionbootstrap "github.com/fleetdm/edr/server/detection/bootstrap"
	endpointbootstrap "github.com/fleetdm/edr/server/endpoint/bootstrap"
	"github.com/fleetdm/edr/server/httpserver"
	identitybootstrap "github.com/fleetdm/edr/server/identity/bootstrap"
	"github.com/fleetdm/edr/server/metrics"
	observabilitybootstrap "github.com/fleetdm/edr/server/observability/bootstrap"
	"github.com/fleetdm/edr/server/tracingpolicy"
	visibilitybootstrap "github.com/fleetdm/edr/server/visibility/bootstrap"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = ""
)

const serviceName = "fleet-edr-ingest"

func main() { bootstrap.Main(run) }

func run() error {
	// The ingest binary serves the highest-volume route (POST /api/events), so capping its trace export is the main win of issue #374.
	// The sampler is built before bootstrap.Init so it is installed on the TracerProvider at OTel init.
	traceSampler := tracingpolicy.NewSampler()
	ctx, env, db, stop, err := bootstrap.Start(bootstrap.Options{
		ServiceName: serviceName, ServiceVersion: version, Commit: commit, BuildTime: buildTime, TraceSampler: traceSampler,
	})
	if err != nil {
		return err
	}
	defer stop()
	cfg, logger := env.Config, env.Logger

	chDB, visibilityCtx, err := openVisibility(ctx, logger, cfg, db)
	if err != nil {
		return err
	}
	defer func() { _ = chDB.Close() }()

	// One root secret seeds every long-lived server-side key, same as fleet-edr-server. Deriving here (rather than skipping the keyring)
	// is mandatory: identity + endpoint bootstrap require the derived keys, and the labels MUST match fleet-edr-server's so a host token
	// or session cookie minted by one binary validates under the other.
	kr, err := keyring.New(cfg.SecretKey)
	if err != nil {
		logger.ErrorContext(ctx, "build keyring", "err", err)
		return fmt.Errorf("build keyring: %w", err)
	}
	// keyring.New holds its own copy of the root and cfg.SecretKey is not read past this point, so zero the config's copy to minimize how
	// long the raw root secret lives in memory (heap dumps, core files).
	clear(cfg.SecretKey)

	identityCtx, err := identitybootstrap.New(ctx, identitybootstrap.Deps{
		DB:                db,
		Logger:            logger,
		SessionSigningKey: kr.Derive(keyring.SessionSigningKeyLabel),
	})
	if err != nil {
		logger.ErrorContext(ctx, "open identity", "err", err)
		return err
	}
	if err := identityCtx.ApplySchema(ctx); err != nil {
		logger.ErrorContext(ctx, "identity schema", "err", err)
		return err
	}

	// The observability context owns the runtime trace-sampler settings (issue #374). The ingest tier serves the highest-volume route
	// (POST /api/events), so it polls the same row to honor an operator's ratio / force-full change without a restart. It does NOT mount
	// the admin endpoint (ingest serves no operator routes), but it still needs the schema + read accessor.
	observabilityCtx, err := observabilitybootstrap.New(observabilitybootstrap.Deps{
		DB:     db,
		Logger: logger,
		AuthZ:  identityCtx.AuthZ(),
		Audit:  identityCtx.AuditRecorder(),
	})
	if err != nil {
		logger.ErrorContext(ctx, "open observability", "err", err)
		return err
	}
	if err := observabilityCtx.ApplySchema(ctx); err != nil {
		logger.ErrorContext(ctx, "observability schema", "err", err)
		return err
	}
	// Prime the sampler from the DB before serving, then poll for later changes.
	tracing.StartSettingsSync(ctx, traceSampler, observabilityCtx.TraceSamplerSettingsReader(), logger)

	// drain is the graceful-shutdown signal, as in fleet-edr-server: SIGTERM flips it so /readyz reports 503 and a load balancer stops
	// routing agent uploads here before RunAndShutdown closes the listener (ADR-0011).
	drain := &httpserver.DrainState{}
	detectionCtx, err := detectionbootstrap.New(detectionbootstrap.Deps{
		DB:         db,
		Logger:     logger,
		Mode:       detectionbootstrap.ModeIntake,
		IsDraining: drain.IsDraining,
		Build: detectionbootstrap.BuildInfo{
			Version:   version,
			Commit:    commit,
			BuildTime: buildTime,
		},
		EventLog:     visibilityCtx.EventLog(),
		EventArchive: visibilityCtx.EventArchive(),
	})
	if err != nil {
		logger.ErrorContext(ctx, "open detection", "err", err)
		return err
	}
	if err := detectionCtx.ApplySchema(ctx); err != nil {
		logger.ErrorContext(ctx, "detection schema", "err", err)
		return err
	}
	metricsRec := wireIngestMetrics(detectionCtx)

	endpointCtx, err := endpointbootstrap.New(endpointbootstrap.Deps{
		DB:                  db,
		Logger:              logger,
		EnrollSecret:        cfg.EnrollSecret,
		EnrollRatePerMinute: cfg.EnrollRatePerMin,
		Audit:               identityCtx.AuditRecorder(),
		AuthZ:               identityCtx.AuthZ(),
		HostTokenLifetime:   config.DefaultHostTokenLifetime,
		HostTokenSigningKey: kr.Derive(keyring.HostTokenSigningLabel),
	})
	if err != nil {
		logger.ErrorContext(ctx, "open endpoint", "err", err)
		return err
	}
	if err := endpointCtx.ApplySchema(ctx); err != nil {
		logger.ErrorContext(ctx, "endpoint schema", "err", err)
		return err
	}

	// Fail closed on the revocation snapshot: it is the only revocation enforcement on the no-DB verify hot path, so an empty snapshot
	// would accept already-revoked/epoch-bumped tokens. A failed initial load (right after the schema apply succeeded) is a real
	// outage, so refuse to serve rather than start allow-all. The background ticker keeps it fresh afterward.
	revSnap := endpointCtx.RevocationSnapshot()
	if rerr := revSnap.Refresh(ctx); rerr != nil {
		return fmt.Errorf("initial revocation snapshot load: %w", rerr)
	}
	go revSnap.Run(ctx, endpointbootstrap.DefaultRevocationRefreshInterval)

	hostTokenMW := endpointCtx.HostTokenMiddleware()

	mux := http.NewServeMux()
	detectionCtx.RegisterHealthRoutes(mux)
	endpointCtx.RegisterPublicRoutes(mux)
	mux.Handle("POST /api/events", hostTokenMW(detectionCtx.Service().IngestHandler()))
	mux.Handle("POST /api/token/refresh", hostTokenMW(endpointCtx.TokenRefreshHandler()))

	srv, err := bootstrap.NewHTTPServer(ctx, cfg, logger, bootstrap.HTTPServerOptions{
		ServiceName: serviceName, Handler: mux, Metrics: metricsRec,
	})
	if err != nil {
		return err
	}
	// The ingest binary serves only the event-intake HTTP surface (no agent control channel), so it passes a nil control mux: no gRPC
	// multiplexing.
	return httpserver.RunAndShutdown(ctx, srv, nil, logger, drain, cfg.ShutdownDrain)
}

// openVisibility opens the required ClickHouse event archive (ADR-0015) and builds the visibility context. The ingest tier fans every
// accepted event out to the archive plus the MySQL EventLog queue, so EDR_CLICKHOUSE_DSN is required here too: an unset value is a fatal
// misconfig, not a MySQL fallback. Returns the ClickHouse handle so the caller can defer its Close.
func openVisibility(ctx context.Context, logger *slog.Logger, cfg *config.Config, db *sqlx.DB) (*sqlx.DB, *visibilitybootstrap.Visibility, error) {
	// EDR_CLICKHOUSE_DSN is validated as required in config.Load (ADR-0015: no MySQL events fallback), so it is non-empty here.
	chDB, err := visibilitybootstrap.OpenClickHouse(ctx, cfg.ClickHouseDSN)
	if err != nil {
		logger.ErrorContext(ctx, "open clickhouse", "err", err)
		return nil, nil, err
	}
	visibilityCtx, err := visibilitybootstrap.New(visibilitybootstrap.Deps{DB: db, ClickHouseDB: chDB, Logger: logger})
	if err != nil {
		_ = chDB.Close()
		logger.ErrorContext(ctx, "open visibility", "err", err)
		return nil, nil, err
	}
	if err := visibilityCtx.ApplySchema(ctx); err != nil {
		_ = chDB.Close()
		logger.ErrorContext(ctx, "visibility schema", "err", err)
		return nil, nil, err
	}
	return chDB, visibilityCtx, nil
}

// metricsSetter is the part of the detection context wireIngestMetrics needs.
type metricsSetter interface {
	SetMetrics(detectionapi.MetricsRecorder)
}

// wireIngestMetrics builds the ingest tier's recorder and hands it to detection, whose intake handler records edr.events.ingested
// and edr.ingest.heartbeats_dropped; the caller also passes it to the HTTP server for request duration. Without it a split
// deployment never counts the events its ingest tier accepts (issue #1263). No gauge source: the host-fleet gauges describe the
// whole deployment and fleet-edr-server already reports them, so registering them here too would double every fleet-wide sum.
func wireIngestMetrics(detection metricsSetter) *metrics.Recorder {
	rec := metrics.New(nil, metrics.Options{})
	detection.SetMetrics(rec)
	return rec
}
