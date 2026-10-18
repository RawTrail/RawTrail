package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/rawtrail/rawtrail/server/auditoutbox"
	"github.com/rawtrail/rawtrail/server/endpoint/api"
	"github.com/rawtrail/rawtrail/server/endpoint/internal/enroll"
	"github.com/rawtrail/rawtrail/server/endpoint/internal/middleware"
	"github.com/rawtrail/rawtrail/server/endpoint/internal/mysql"
	"github.com/rawtrail/rawtrail/server/endpoint/internal/operator"
	"github.com/rawtrail/rawtrail/server/endpoint/internal/revocation"
	"github.com/rawtrail/rawtrail/server/endpoint/internal/service"
	"github.com/rawtrail/rawtrail/server/endpoint/internal/signedtoken"
	"github.com/rawtrail/rawtrail/server/endpoint/internal/status"
	"github.com/rawtrail/rawtrail/server/endpoint/internal/token"
	endpointmigrations "github.com/rawtrail/rawtrail/server/endpoint/migrations"
	"github.com/rawtrail/rawtrail/server/httpserver"
	identityapi "github.com/rawtrail/rawtrail/server/identity/api"
	"github.com/rawtrail/rawtrail/server/migrations/runner"
)

// hostTokenKeyID labels the active host-token signing key; it is carried in every token's claims and checked on verify. Bump when
// rotating the signing key (and add an overlap verifier for the old id).
const hostTokenKeyID = "v1"

// DefaultRevocationRefreshInterval is how often each replica reloads the revocation snapshot from the database. It bounds the
// worst-case staleness of a revocation across replicas. 5s keeps a kill switch effectively immediate without
// hammering the DB (one small query per replica per interval).
const DefaultRevocationRefreshInterval = 5 * time.Second

// Deps bundles what New needs to wire the endpoint context. cmd/main owns the *sqlx.DB handle and shares it across every context's
// bootstrap.
type Deps struct {
	DB                  *sqlx.DB
	Logger              *slog.Logger
	EnrollSecret        string
	EnrollRatePerMinute int
	// HostTokenSigningKey is the server-held HMAC key that signs + verifies self-validating host tokens. Required, at least 32 bytes.
	// cmd/main derives it from the deployment root secret (EDR_SECRET_KEY) via internal/keyring. Changing the root (or the derivation
	// label) invalidates every outstanding token: a fleet-wide re-enroll.
	HostTokenSigningKey []byte

	// Audit is the operator-action recorder. Optional: a revoke still commits its audit entry without one, and the entry then waits in
	// the outbox undelivered rather than being discarded. cmd/main wires identityCtx.AuditRecorder().
	Audit identityapi.AuditRecorder

	// AuditSweepInterval is how often this context's audit outbox sweep delivers entries a request could not. Zero or negative means
	// auditoutbox.DefaultSweepInterval. Tests shorten it to watch a sweep deliver.
	AuditSweepInterval time.Duration

	// AuthZ is the authorization chokepoint every privileged operator
	// route gates on. Required. cmd/main wires identityCtx.AuthZ().
	AuthZ identityapi.AuthZ

	// HostTokenLifetime is the TTL of a minted signed host token: how long it is valid before the agent must refresh it. Zero -> service
	// default (60m). cmd/main passes the fixed config.DefaultHostTokenLifetime (60m); it is no longer operator-tunable.
	HostTokenLifetime time.Duration
}

// Endpoint is the handle cmd/main holds for the endpoint bounded context. It exposes the Service for cross-context callers (today:
// metrics' EnrolledHosts gauge), the host-token middleware factory, the route-registration methods, and ApplySchema.
type Endpoint struct {
	svc         api.Service
	enrollH     *enroll.Handler
	operatorH   *operator.Handler
	tokenH      *token.Handler
	statusH     *status.Handler
	hostTokenMW func(http.Handler) http.Handler
	snapshot    *revocation.Snapshot
	db          *sqlx.DB
	// store is held so this context can expose narrow write surfaces another context depends on (HealthEpisodeRecorder), without
	// handing out the whole store.
	store  *mysql.Store
	logger *slog.Logger
	// auditDrain turns the audit entries a revoke commits into audit rows (issue #1225). Nil when no recorder is wired.
	auditDrain         *auditoutbox.Drain
	auditSweepInterval time.Duration
}

// New wires the endpoint context. Does NOT apply the schema (call ApplySchema for that). Returns an error if Deps is missing required
// fields.
func New(deps Deps) (*Endpoint, error) {
	if deps.DB == nil {
		return nil, errors.New("endpoint bootstrap: DB is required")
	}
	if deps.EnrollSecret == "" {
		return nil, errors.New("endpoint bootstrap: EnrollSecret is required")
	}
	if len(deps.HostTokenSigningKey) < 32 {
		return nil, errors.New("endpoint bootstrap: HostTokenSigningKey is required (at least 32 bytes)")
	}
	if deps.AuthZ == nil {
		return nil, errors.New("endpoint bootstrap: AuthZ is required")
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	signer, err := signedtoken.New(deps.HostTokenSigningKey, hostTokenKeyID)
	if err != nil {
		return nil, fmt.Errorf("endpoint bootstrap: host token signer: %w", err)
	}
	store := mysql.NewStore(deps.DB)
	snapshot := revocation.NewSnapshot(store, logger)
	outbox := auditoutbox.NewStore(deps.DB, mysql.AuditOutboxTable)
	var auditDrain *auditoutbox.Drain
	if deps.Audit != nil {
		if auditDrain, err = auditoutbox.NewDrain(outbox, deps.Audit, "enrollment actions", logger); err != nil {
			return nil, fmt.Errorf("endpoint bootstrap: audit drain: %w", err)
		}
	}
	svc := service.New(service.Options{
		Store:       store,
		Secret:      deps.EnrollSecret,
		Signer:      signer,
		Revocations: snapshot,
		AuditOutbox: outbox,
		AuditDrain:  auditDrain,
		TokenTTL:    deps.HostTokenLifetime,
		Logger:      logger,
	})

	opH := operator.New(svc, deps.AuthZ, logger)
	return &Endpoint{
		svc:   svc,
		store: store,
		enrollH: enroll.New(svc, enroll.Options{
			RatePerMinute: deps.EnrollRatePerMinute,
			Logger:        logger,
		}),
		operatorH:   opH,
		tokenH:      token.New(svc, logger),
		statusH:     status.New(svc, logger),
		hostTokenMW: middleware.HostToken(svc, logger),
		snapshot:    snapshot,
		db:          deps.DB,
		logger:      logger,

		auditDrain:         auditDrain,
		auditSweepInterval: deps.AuditSweepInterval,
	}, nil
}

// ApplySchema applies endpoint's goose migration corpus. Idempotent (goose skips already-applied versions). No cross-context FKs;
// ordering with other contexts' ApplySchema is not load-bearing.
func (e *Endpoint) ApplySchema(ctx context.Context) error {
	return ApplySchema(ctx, e.db)
}

// ApplySchema is the package-level form: applies endpoint's goose migration corpus against the given DB without requiring a fully
// constructed *Endpoint. Used by server/testdb so tests can apply every context's schema without faking out each bootstrap's
// service dependencies. Idempotent (goose skips already-applied versions), so a second call on an already-migrated DB is a no-op.
func ApplySchema(ctx context.Context, db *sqlx.DB) error {
	if db == nil {
		return errors.New("endpoint ApplySchema: db must not be nil")
	}
	return runner.Up(ctx, db, endpointmigrations.FS, runner.Options{
		Context:   "endpoint",
		TableName: "endpoint_goose_db_version",
	})
}

// Service exposes the public api.Service for cross-context callers. Today: cmd/main's serverGaugeSource calls Service().CountActive
// for the EnrolledHosts metrics gauge.
func (e *Endpoint) Service() api.Service { return e.svc }

// HostTokenMiddleware returns the per-request middleware that gates agent endpoints. cmd/main chains this on POST /api/events,
// GET /api/commands, PUT /api/commands/{id}, and POST /api/token/refresh.
func (e *Endpoint) HostTokenMiddleware() func(http.Handler) http.Handler { return e.hostTokenMW }

// TokenRefreshHandler returns the POST /api/token/refresh handler. cmd/main mounts it inside the host-token-protected mux so it shares
// the same authentication as the other agent routes.
func (e *Endpoint) TokenRefreshHandler() http.Handler { return e.tokenH }

// StatusHandler returns the POST /api/status agent-health check-in handler. cmd/main mounts it inside the host-token-protected mux so it
// shares the same authentication as the other agent routes; the handler reads the host_id the middleware pinned.
func (e *Endpoint) StatusHandler() http.Handler { return e.statusH }

// HealthEpisodeRecorder exposes where a component fault that needs a person is recorded (issue #778). The detection engine holds
// it: the engine is what evaluates the agent's report and decides the finding is a health signal rather than a detection, while
// host health is this context's to own, so the engine states the fact and this context decides what storing it means.
func (e *Endpoint) HealthEpisodeRecorder() api.HealthEpisodeRecorder { return e.store }

// RevocationSnapshot returns the per-replica revocation snapshot so cmd/main can trigger an initial synchronous load before serving and
// run the background refresh loop. Per ADR-0010 it is a perf cache, safe to lose.
func (e *Endpoint) RevocationSnapshot() *revocation.Snapshot { return e.snapshot }

// RegisterPublicRoutes wires POST /api/enroll. Public because the agent
// has no token yet; the handler does its own per-IP rate limit + audit.
func (e *Endpoint) RegisterPublicRoutes(mux *http.ServeMux) {
	e.enrollH.RegisterRoutes(mux)
}

// RegisterAuthedRoutes wires the operator-facing routes: GET /api/enrollments and POST /api/enrollments/{host_id}/revoke. Caller wraps
// in identity Session + CSRF middleware before mounting.
func (e *Endpoint) RegisterAuthedRoutes(mux httpserver.Router) {
	e.operatorH.RegisterRoutes(mux)
}

// RunAuditSweep delivers the audit entries a revoke committed but whose request could not write out, until ctx is cancelled. It returns
// at once when no recorder is wired.
//
// Run wherever the operator routes might have been mounted by any replica, because entries outlive the wiring that wrote them: a sweep
// over an empty table costs one query a minute.
func (e *Endpoint) RunAuditSweep(ctx context.Context) {
	if e.auditDrain == nil {
		return
	}
	e.auditDrain.SweepLoop(ctx, e.auditSweepInterval)
}
