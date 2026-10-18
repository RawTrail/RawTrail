package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/rawtrail/rawtrail/server/endpoint/internal/revocation"
)

// Enrollment mirrors the `enrollments` row shape used by admin listings. Token verification goes through the signed-token path in the
// service layer, not direct row access.
type Enrollment struct {
	HostID       string     `db:"host_id" json:"host_id"`
	Hostname     string     `db:"hostname" json:"hostname"`
	AgentVersion string     `db:"agent_version" json:"agent_version"`
	OSVersion    string     `db:"os_version" json:"os_version"`
	Platform     string     `db:"platform" json:"platform"`
	SourceIP     string     `db:"source_ip" json:"source_ip"`
	EnrolledAt   time.Time  `db:"enrolled_at" json:"enrolled_at"`
	ExpiresAt    *time.Time `db:"expires_at" json:"expires_at,omitempty"`
	RevokedAt    *time.Time `db:"revoked_at" json:"revoked_at,omitempty"`
	RevokeReason *string    `db:"revoke_reason" json:"revoke_reason,omitempty"`
	RevokedBy    *string    `db:"revoked_by" json:"revoked_by,omitempty"`
}

// Store owns the `enrollments` table. It is backed by *sqlx.DB (the store package already opens one via otelsql.Open); we take a db
// handle rather than the full *store.Store so this package stays unit-testable with a plain sqlx.DB.
type Store struct {
	db *sqlx.DB
}

// NewStore constructs an enrollment store over an existing sqlx.DB handle.
func NewStore(db *sqlx.DB) *Store {
	return &Store{db: db}
}

// RegisterRequest captures the inputs to a successful enrollment. The presented secret has already been validated by the handler; by
// the time Register is called we are committed to creating the enrollment row.
type RegisterRequest struct {
	HostID       string
	Hostname     string
	AgentVersion string
	OSVersion    string
	Platform     string
	SourceIP     string
}

// ActiveEnrollment is an active enrollment's host and when it last enrolled.
type ActiveEnrollment struct {
	HostID     string    `db:"host_id"`
	EnrolledAt time.Time `db:"enrolled_at"`
}

// RegisterResult carries the host id, enrollment timestamp, and the epoch the row now holds back to the caller. The bearer token is no
// longer minted here: the service layer mints a self-validating signed token (see internal/signedtoken) at this epoch. Epoch is 0 for a
// brand-new host and the preserved (possibly operator-bumped) value for a re-enroll.
type RegisterResult struct {
	HostID     string
	EnrolledAt time.Time
	Epoch      int64
}

// Register upserts the enrollment row keyed by host_id: a brand-new host inserts a fresh row at token_epoch 0; a re-enroll updates the
// metadata IN PLACE. token_epoch is deliberately NOT in the UPDATE list, so it is PRESERVED across a re-enroll. That is load-bearing for
// revocation: the epoch bump a revoke makes must survive the agent's automatic re-enroll, or a token stolen before the revoke would
// become valid again the moment the host re-enrolls (the re-enroll would otherwise reset the epoch back to 0). Revocation is cleared so
// a host that proves the enroll secret is admitted afresh. The enrollments table holds the *current* enrollment state only; revocation
// history lives in the audit trail and structured logs.
//
// enrolled_at comes from the database clock, the clock every command's created_at comes from, because the watched-path catch-up
// (issue #998) orders the two: a command queued before a host's latest enrollment may not have survived the reinstall behind it, and
// comparing a database time with an application time would let clock skew hide that.
func (s *Store) Register(ctx context.Context, req RegisterRequest) (*RegisterResult, error) {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO enrollments (host_id, hostname, agent_version, os_version, platform, source_ip, enrolled_at)
		VALUES (?, ?, ?, ?, ?, ?, NOW(6))
		ON DUPLICATE KEY UPDATE
			hostname      = VALUES(hostname),
			agent_version = VALUES(agent_version),
			os_version    = VALUES(os_version),
			platform      = VALUES(platform),
			source_ip     = VALUES(source_ip),
			enrolled_at   = NOW(6),
			revoked_at    = NULL,
			revoke_reason = NULL,
			revoked_by    = NULL
	`, req.HostID, req.Hostname, req.AgentVersion, req.OSVersion, req.Platform, req.SourceIP); err != nil {
		return nil, fmt.Errorf("upsert enrollment: %w", err)
	}

	// Read back the epoch the row now carries (0 for a fresh host, the preserved value for a re-enroll). Sourced from the DB rather than
	// assumed 0 so the minted token matches the host's current epoch and survives the revocation-snapshot check.
	var row struct {
		Epoch      int64     `db:"token_epoch"`
		EnrolledAt time.Time `db:"enrolled_at"`
	}
	if err := s.db.GetContext(ctx, &row, `SELECT token_epoch, enrolled_at FROM enrollments WHERE host_id = ?`, req.HostID); err != nil {
		return nil, fmt.Errorf("read token epoch: %w", err)
	}

	return &RegisterResult{HostID: req.HostID, EnrolledAt: row.EnrolledAt.UTC(), Epoch: row.Epoch}, nil
}

// CountActive returns how many non-revoked enrollments exist. Cheaper than ActiveHostIDs when the caller only needs the count. The
// OTel gauge `edr.enrolled.hosts` is the primary caller.
func (s *Store) CountActive(ctx context.Context) (int, error) {
	var n int
	if err := s.db.GetContext(ctx, &n, `SELECT COUNT(*) FROM enrollments WHERE revoked_at IS NULL`); err != nil {
		return 0, fmt.Errorf("count active enrollments: %w", err)
	}
	return n, nil
}

// ActiveHostIDs returns the host_id of every currently-active (non-revoked) enrollment. Used to fan out policy updates to the set of
// hosts that still have a valid token. It projects ActiveEnrollments, so a push and the watched-path catch-up agree on which
// enrollments are active.
func (s *Store) ActiveHostIDs(ctx context.Context) ([]string, error) {
	rows, err := s.ActiveEnrollments(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.HostID
	}
	return ids, nil
}

// ActiveEnrollments returns each active (non-revoked) enrollment's host_id and enrolled_at, in host_id order, and nothing else: the
// watched-path catch-up reads it every few minutes, so it reads only the columns that question needs. host_id is the primary key, so
// the table holds one row per host and the read is bounded by the number of hosts ever enrolled.
func (s *Store) ActiveEnrollments(ctx context.Context) ([]ActiveEnrollment, error) {
	var rows []ActiveEnrollment
	if err := s.db.SelectContext(ctx, &rows, `
		SELECT host_id, enrolled_at FROM enrollments WHERE revoked_at IS NULL ORDER BY host_id
	`); err != nil {
		return nil, fmt.Errorf("list active enrollments: %w", err)
	}
	return rows, nil
}

// List returns every enrollment row, active + revoked, for the admin UI.
func (s *Store) List(ctx context.Context) ([]Enrollment, error) {
	var rows []Enrollment
	err := s.db.SelectContext(ctx, &rows, `
		SELECT host_id, hostname, agent_version, os_version, platform, source_ip,
		       enrolled_at, expires_at, revoked_at, revoke_reason, revoked_by
		FROM enrollments
		ORDER BY enrolled_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list enrollments: %w", err)
	}
	return rows, nil
}

// Get returns a single enrollment. Returns sql.ErrNoRows when absent.
func (s *Store) Get(ctx context.Context, hostID string) (*Enrollment, error) {
	var e Enrollment
	err := s.db.GetContext(ctx, &e, `
		SELECT host_id, hostname, agent_version, os_version, platform, source_ip,
		       enrolled_at, expires_at, revoked_at, revoke_reason, revoked_by
		FROM enrollments
		WHERE host_id = ?
	`, hostID)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// AuditOutboxTable is this context's audit outbox. An operator action commits its audit entry into it, in the transaction that makes
// the change (issue #1225), and the shared drain in server/auditoutbox turns the entries into audit rows.
const AuditOutboxTable = "endpoint_audit_outbox"

// Revoke marks a host's enrollment revoked AND bumps its token_epoch, then runs audit in the same transaction so the revoke and the
// entry recording it commit together or not at all. Returns ErrNotFound when the host has no enrollment row.
//
// The epoch bump is what makes a revoke stick (issue #1225). The agent re-enrolls with the fleet-wide enroll secret as soon as its token
// is refused, and Register clears revoked_at while preserving token_epoch. Without the bump, a token copied off the host before the
// revoke would be valid again after that re-enroll, and the refresh path would keep renewing it.
//
// Repeating a revoke bumps the epoch again, which is harmless, and preserves the first revoker's revoked_at, reason and actor: COALESCE
// only fills columns that are still NULL. The epoch changes on every call, so RowsAffected is 1 for any existing row and 0 only for an
// unknown host.
func (s *Store) Revoke(ctx context.Context, hostID, reason, actor string, audit func(q sqlx.ExecerContext) error) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin revoke transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `
		UPDATE enrollments
		SET revoked_at    = COALESCE(revoked_at, ?),
		    revoke_reason = COALESCE(revoke_reason, ?),
		    revoked_by    = COALESCE(revoked_by, ?),
		    token_epoch   = token_epoch + 1
		WHERE host_id = ?
	`, time.Now().UTC(), reason, actor, hostID)
	if err != nil {
		return fmt.Errorf("revoke enrollment: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke enrollment rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	if err := audit(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit revoke transaction: %w", err)
	}
	return nil
}

// TokenStatus returns a host's current token_epoch and whether it is revoked. Used by the refresh path (not the hot verify path) to
// mint a new token at the host's current epoch and to refuse refresh for a revoked host. Returns ErrNotFound when the host has no
// enrollment row.
func (s *Store) TokenStatus(ctx context.Context, hostID string) (epoch int64, revoked bool, err error) {
	var row struct {
		Epoch   int64 `db:"token_epoch"`
		Revoked int   `db:"revoked"`
	}
	e := s.db.GetContext(ctx, &row, `
		SELECT token_epoch, IF(revoked_at IS NOT NULL, 1, 0) AS revoked
		FROM enrollments
		WHERE host_id = ?
	`, hostID)
	if errors.Is(e, sql.ErrNoRows) {
		return 0, false, ErrNotFound
	}
	if e != nil {
		return 0, false, fmt.Errorf("token status: %w", e)
	}
	return row.Epoch, row.Revoked != 0, nil
}

// RevocationEntries returns every host that is revoked or has a non-zero token_epoch: the minimal set the revocation snapshot needs to
// reject cut-off or cycled tokens. The bulk of a fleet is neither, so the result stays small even at large host counts. Implements
// revocation.Source.
func (s *Store) RevocationEntries(ctx context.Context) ([]revocation.Entry, error) {
	var rows []struct {
		HostID  string `db:"host_id"`
		Epoch   int64  `db:"token_epoch"`
		Revoked int    `db:"revoked"`
	}
	// IF(...,1,0) rather than the bare boolean expression: the MySQL driver yields int64 for a boolean column expression, which
	// database/sql will not Scan into a Go bool, so we select an explicit int and map it below.
	if err := s.db.SelectContext(ctx, &rows, `
		SELECT host_id, token_epoch, IF(revoked_at IS NOT NULL, 1, 0) AS revoked
		FROM enrollments
		WHERE token_epoch > 0 OR revoked_at IS NOT NULL
	`); err != nil {
		return nil, fmt.Errorf("load revocation entries: %w", err)
	}
	out := make([]revocation.Entry, len(rows))
	for i, r := range rows {
		out[i] = revocation.Entry{ID: r.HostID, Epoch: r.Epoch, Revoked: r.Revoked != 0}
	}
	return out, nil
}

// ErrNotFound mirrors sql.ErrNoRows without leaking the database concept to callers that
// do not import database/sql.
var ErrNotFound = errors.New("enrollment: not found")
