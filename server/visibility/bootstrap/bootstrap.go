package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jmoiron/sqlx"

	"github.com/rawtrail/rawtrail/server/migrations/runner"
	"github.com/rawtrail/rawtrail/server/visibility/api"
	"github.com/rawtrail/rawtrail/server/visibility/internal/clickhouse"
	"github.com/rawtrail/rawtrail/server/visibility/internal/eventlog"
	visibilitymigrations "github.com/rawtrail/rawtrail/server/visibility/migrations"
	migrationsclickhouse "github.com/rawtrail/rawtrail/server/visibility/migrations-clickhouse"
)

// Deps bundles what New needs to wire the visibility context.
type Deps struct {
	// DB is the MySQL pool backing the EventLog work queue and the control plane. Required.
	DB *sqlx.DB
	// ClickHouseDB is the ClickHouse pool backing the EventArchive (opened via clickhouse.Open). Optional: when nil, the context runs
	// without an archive (queue only) and EventArchive returns nil. The cutover wires it in cmd/main.
	ClickHouseDB *sqlx.DB
	// EventRetentionDays is how long the archive keeps events: ApplySchema sets its TTL to it (issue #1434). 0 leaves the TTL the
	// migrations set.
	EventRetentionDays int
	Logger             *slog.Logger
}

// Visibility is the handle cmd/main holds for the visibility context.
type Visibility struct {
	eventLog     *eventlog.Store
	eventArchive *clickhouse.Store
	db           *sqlx.DB
	chDB         *sqlx.DB
	retention    int
	logger       *slog.Logger
}

// OpenClickHouse dials the ClickHouse event archive and returns the pool, exposed here so cmd/main can open it without importing the
// visibility-internal clickhouse package (the Go internal rule blocks that). dsn is a clickhouse-go DSN; closing the handle is the
// caller's responsibility. Pass the returned pool to New as Deps.ClickHouseDB.
func OpenClickHouse(ctx context.Context, dsn string) (*sqlx.DB, error) {
	return clickhouse.Open(ctx, dsn)
}

// New wires the visibility context. It does NOT apply the schema (call ApplySchema).
func New(deps Deps) (*Visibility, error) {
	if deps.DB == nil {
		return nil, errors.New("visibility bootstrap: DB is required")
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	el, err := eventlog.New(deps.DB)
	if err != nil {
		return nil, fmt.Errorf("visibility bootstrap: %w", err)
	}
	v := &Visibility{eventLog: el, db: deps.DB, chDB: deps.ClickHouseDB, retention: deps.EventRetentionDays, logger: logger}
	if deps.ClickHouseDB != nil {
		archive, err := clickhouse.New(deps.ClickHouseDB)
		if err != nil {
			return nil, fmt.Errorf("visibility bootstrap: %w", err)
		}
		v.eventArchive = archive
	}
	return v, nil
}

// EventLog returns the durable work queue that decouples ingestion from detection processing.
func (v *Visibility) EventLog() api.EventLog { return v.eventLog }

// EventArchive returns the durable event lake, or nil when no ClickHouse connection was provided.
func (v *Visibility) EventArchive() api.EventArchive {
	if v.eventArchive == nil {
		return nil
	}
	return v.eventArchive
}

// ApplySchema runs visibility's migrations: the MySQL queue always, and the ClickHouse event archive when a ClickHouse connection was
// provided, whose TTL it then sets to the event retention. Idempotent.
func (v *Visibility) ApplySchema(ctx context.Context) error {
	if err := ApplySchema(ctx, v.db); err != nil {
		return err
	}
	if v.chDB == nil {
		return nil
	}
	if err := ApplyClickHouseSchema(ctx, v.chDB); err != nil {
		return err
	}
	if v.retention == 0 {
		return nil
	}
	changed, err := clickhouse.ApplyEventRetention(ctx, v.chDB, v.retention)
	if err != nil {
		return fmt.Errorf("visibility: event retention: %w", err)
	}
	if changed {
		v.logger.InfoContext(ctx, "event retention set", "edr.event_retention_days", v.retention)
	}
	return nil
}

// ApplySchema is the package-level form: applies visibility's MySQL goose migration corpus (the event_queue) against db. Used by the
// test fixture composer. Idempotent (goose skips already-applied versions).
func ApplySchema(ctx context.Context, db *sqlx.DB) error {
	if db == nil {
		return errors.New("visibility ApplySchema: db must not be nil")
	}
	return runner.Up(ctx, db, visibilitymigrations.FS, runner.Options{
		Context:   "visibility",
		TableName: "visibility_goose_db_version",
	})
}

// ApplyClickHouseSchema applies the event-archive ClickHouse migration corpus against chDB. Idempotent.
func ApplyClickHouseSchema(ctx context.Context, chDB *sqlx.DB) error {
	if chDB == nil {
		return errors.New("visibility ApplyClickHouseSchema: db must not be nil")
	}
	return runner.Up(ctx, chDB, migrationsclickhouse.FS, runner.Options{
		Context:   "visibility",
		TableName: "visibility_clickhouse_goose_db_version",
		Dialect:   runner.DialectClickHouse,
	})
}
