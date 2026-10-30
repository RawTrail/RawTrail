package hostpush

import (
	"context"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/rawtrail/rawtrail/internal/versionedset"
	"github.com/rawtrail/rawtrail/server/auditoutbox"
	"github.com/rawtrail/rawtrail/server/catchup"
	"github.com/rawtrail/rawtrail/server/rules/api"
)

// AuditHold is how long a replacement's audit entry waits for the push's host counts: the push takes well under a second, so the hold
// only matters when the replica died before adding the counts.
const AuditHold = 5 * time.Minute

// Document is a fleet-wide document kept in one versionedset row, such as the agent settings (issue #1426): read, replaced with its
// audit entry in the same transaction, and sealed once the push has its host counts. How it is encoded and decoded is its owner's.
type Document[T any] struct {
	set      *versionedset.Store
	outbox   *auditoutbox.Store
	decode   func(versionedset.Row) (T, error)
	conflict error
}

// NewDocument builds a Document over table, auditing through outboxTable. conflict is returned by a conditional replacement of a
// document that changed since it was read. Panics on a nil db, which is a wiring bug.
func NewDocument[T any](
	db *sqlx.DB, table versionedset.Table, outboxTable string, decode func(versionedset.Row) (T, error), conflict error,
) *Document[T] {
	if db == nil {
		panic("hostpush.NewDocument: db must not be nil")
	}
	return &Document[T]{
		set: versionedset.New(db, table), outbox: auditoutbox.NewStore(db, outboxTable), decode: decode, conflict: conflict,
	}
}

// Get returns the stored document.
func (d *Document[T]) Get(ctx context.Context) (T, error) {
	row, err := d.set.Get(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	return d.decode(row)
}

// ReplaceEncoded stores encoded as the document's new row, as ReplaceAudited does, holding the audit entry for AuditHold.
func (d *Document[T]) ReplaceEncoded(
	ctx context.Context, encoded []byte, actor string, expectedVersion *int64, audit func(previous, next T) (auditoutbox.Entry, error),
) (previous, next T, auditID int64, err error) {
	return ReplaceAudited(ctx, d.set, d.outbox, encoded, actor, expectedVersion, d.conflict, AuditHold, d.decode, audit)
}

// SealAudit adds the push's host counts to a replacement's held audit entry and releases it for delivery. It reports false when the
// hold had already passed, in which case the entry is delivered without the counts.
func (d *Document[T]) SealAudit(ctx context.Context, id int64, entry auditoutbox.Entry) (bool, error) {
	return d.outbox.Seal(ctx, id, entry)
}

// Converger queues a Document's current version for the hosts that do not have it: a host enrolled after a change, one that
// reinstalled, and one whose command aged out undelivered.
type Converger[T any] struct {
	get     func(ctx context.Context) (T, error)
	version func(T) int64
	payload func(T) []byte
	carries func(payload []byte, current T) bool
	sweep   Catchup
}

// ConvergerConfig is what a Converger is built from. Get reads the current document; Version, Payload and Carries are its version, the
// command payload that delivers it, and whether a queued payload delivers it. Every field but Logger is required.
type ConvergerConfig[T any] struct {
	Subject     string
	CommandType string
	Get         func(ctx context.Context) (T, error)
	Version     func(T) int64
	Payload     func(T) []byte
	Carries     func(payload []byte, current T) bool
	Commands    func(ctx context.Context, hostIDs []string, commandType string, payload []byte) (int, error)
	Enrollments api.HostEnrollmentLister
	Latest      api.HostCommandLister
	Logger      *slog.Logger
}

// NewConverger builds a Converger. Panics on a missing dependency, which is a wiring bug.
func NewConverger[T any](cfg ConvergerConfig[T]) *Converger[T] {
	if cfg.Get == nil || cfg.Version == nil || cfg.Payload == nil || cfg.Carries == nil || cfg.Commands == nil ||
		cfg.Enrollments == nil || cfg.Latest == nil {
		panic("hostpush.NewConverger: " + cfg.Subject + ": every dependency but the logger is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Converger[T]{get: cfg.Get, version: cfg.Version, payload: cfg.Payload, carries: cfg.Carries, sweep: Catchup{
		Subject: cfg.Subject, CommandType: cfg.CommandType, Commands: cfg.Commands, Enrollments: cfg.Enrollments, Latest: cfg.Latest,
		Logger: logger, Now: time.Now,
	}}
}

// Loop runs Converge every interval until ctx is cancelled; a zero or negative interval means catchup.DefaultInterval. Not
// leader-gated, for the watched-path catch-up's reason: a race between replicas queues a duplicate the agent turns away as not newer.
func (c *Converger[T]) Loop(ctx context.Context, interval time.Duration) {
	catchup.Loop(ctx, c.sweep.Subject, c.Converge, interval, c.sweep.Logger)
}

// Converge queues the current document for every enrolled host that needs it and returns how many hosts it queued it for. The
// document no operator has changed is sent too, so a host learns the fleet's rather than keeping whatever it last had.
func (c *Converger[T]) Converge(ctx context.Context) (int, error) {
	current, err := c.get(ctx)
	if err != nil {
		return 0, err
	}
	return c.sweep.Converge(ctx, c.version(current), c.payload(current), func(payload []byte) bool {
		return c.carries(payload, current)
	})
}
