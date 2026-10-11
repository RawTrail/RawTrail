package mysql_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fleetdm/edr/server/endpoint/internal/mysql"
	"github.com/fleetdm/edr/server/endpoint/internal/revocation"
	"github.com/fleetdm/edr/server/endpoint/testkit"
	"github.com/fleetdm/edr/server/testdb"
)

const testUUID = "93DFC6F5-763D-5075-B305-8AC145D12F96"

// newTestStore opens an isolated DB and applies endpoint's schema via the canonical testkit.ApplySchema. Lives in the external test
// package so the testdb -> endpoint/bootstrap -> endpoint/internal/mysql cycle doesn't bite.
func newTestStore(t *testing.T) *mysql.Store {
	t.Helper()
	s, _ := newTestStoreWithDB(t)
	return s
}

// newTestStoreWithDB is newTestStore plus the handle, for a test that needs to read back what was STORED rather than what a write
// method returned. Beside newTestStore rather than in the test that needs it, so the two cannot drift in how they build the schema.
func newTestStoreWithDB(t *testing.T) (*mysql.Store, *sqlx.DB) {
	t.Helper()
	db := testdb.Open(t)
	require.NoError(t, testkit.ApplySchema(t.Context(), db))
	return mysql.NewStore(db), db
}

// noAudit is the audit step of a revoke for store tests that are not about the audit entry.
func noAudit(sqlx.ExecerContext) error { return nil }

func register(t *testing.T, s *mysql.Store, hostID string) {
	t.Helper()
	_, err := s.Register(t.Context(), mysql.RegisterRequest{
		HostID: hostID, Hostname: "h", AgentVersion: "v", OSVersion: "o", SourceIP: "127.0.0.1",
	})
	require.NoError(t, err)
}

func TestRegister_HappyPath(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	res, err := s.Register(t.Context(), mysql.RegisterRequest{
		HostID: testUUID, Hostname: "qa-host", AgentVersion: "0.0.1-dev", OSVersion: "macOS 15.3", SourceIP: "127.0.0.1",
	})
	require.NoError(t, err)
	assert.Equal(t, testUUID, res.HostID)
	assert.WithinDuration(t, time.Now(), res.EnrolledAt, 2*time.Second)

	// A fresh enrollment is at epoch 0 and not revoked.
	epoch, revoked, err := s.TokenStatus(t.Context(), testUUID)
	require.NoError(t, err)
	assert.Zero(t, epoch)
	assert.False(t, revoked)
}

// spec:agent-enrollment/host-token-revocation-is-enforced-per-replica/re-enrollment-preserves-the-token-epoch
//
// TestRegister_ReenrollPreservesEpoch: a re-enroll updates the row in place and MUST NOT reset token_epoch (the old REPLACE INTO did,
// which let a token stolen before a revoke validate again once the agent re-enrolled). It must, however, still clear revocation so a
// re-enrolled host is active again.
func TestRegister_ReenrollPreservesEpoch(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := t.Context()
	register(t, s, testUUID)
	require.NoError(t, s.Revoke(ctx, testUUID, "x", "y", noAudit))

	res, err := s.Register(ctx, mysql.RegisterRequest{
		HostID: testUUID, Hostname: "h-reimaged", AgentVersion: "v", OSVersion: "o", SourceIP: "127.0.0.2",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.Epoch, "re-enroll returns the preserved epoch instead of resetting to 0")

	epoch, revoked, err := s.TokenStatus(ctx, testUUID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), epoch, "the DB row still carries the bumped epoch after re-enroll")
	assert.False(t, revoked, "re-enroll clears revoked_at")
}

// TestRevoke_BumpsEpochEveryTime: every revoke bumps token_epoch, a repeat included, so a token minted before any of them stays below the
// host's epoch after the agent re-enrolls (issue #1225).
func TestRevoke_BumpsEpochEveryTime(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := t.Context()
	register(t, s, testUUID)
	require.NoError(t, s.Revoke(ctx, testUUID, "compromised", "op", noAudit))
	require.NoError(t, s.Revoke(ctx, testUUID, "again", "op", noAudit))
	epoch, revoked, err := s.TokenStatus(ctx, testUUID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), epoch)
	assert.True(t, revoked)
}

// spec:agent-enrollment/revoke-ends-prior-credentials/revoke-audit-entry-commits-with-the-revoke
//
// TestRevoke_AuditInTheSameTransaction: the audit step runs on the revoke's own transaction, so an entry it writes commits with the
// revoke, and a failing audit step leaves neither the revoke nor the epoch bump behind.
func TestRevoke_AuditInTheSameTransaction(t *testing.T) {
	t.Parallel()
	s, db := newTestStoreWithDB(t)
	ctx := t.Context()
	register(t, s, testUUID)

	auditFailure := errors.New("audit entry refused")
	err := s.Revoke(ctx, testUUID, "compromised", "op", func(sqlx.ExecerContext) error { return auditFailure })
	require.ErrorIs(t, err, auditFailure)
	epoch, revoked, err := s.TokenStatus(ctx, testUUID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), epoch, "a refused audit entry rolls the epoch bump back")
	assert.False(t, revoked, "a refused audit entry rolls the revoke back")

	// The audit step writes through the transaction it is handed: its row is visible once Revoke returns, and only then.
	require.NoError(t, s.Revoke(ctx, testUUID, "compromised", "op", func(q sqlx.ExecerContext) error {
		var outside int
		require.NoError(t, db.GetContext(ctx, &outside, `SELECT COUNT(*) FROM enrollments WHERE host_id = ? AND revoked_at IS NOT NULL`,
			testUUID))
		assert.Zero(t, outside, "the revoke is not visible outside its transaction before it commits")
		_, err := q.ExecContext(ctx, `INSERT INTO `+mysql.AuditOutboxTable+` (kind, payload) VALUES ('test', '{}')`)
		return err
	}))
	var entries int
	require.NoError(t, db.GetContext(ctx, &entries, `SELECT COUNT(*) FROM `+mysql.AuditOutboxTable))
	assert.Equal(t, 1, entries)
}

func TestTokenStatus_NotFound(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	_, _, err := s.TokenStatus(t.Context(), "00000000-0000-0000-0000-000000000000")
	assert.ErrorIs(t, err, mysql.ErrNotFound)
}

// TestRevocationEntries: a clean host is excluded; an epoch-bumped host carries its epoch; a revoked host is flagged.
func TestRevocationEntries(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := t.Context()
	clean := "11111111-1111-1111-1111-111111111111"
	bumped := "22222222-2222-2222-2222-222222222222"
	revoked := "33333333-3333-3333-3333-333333333333"
	register(t, s, clean)
	register(t, s, bumped)
	register(t, s, revoked)
	// A revoked host that re-enrolled is no longer revoked but keeps the bumped epoch.
	require.NoError(t, s.Revoke(ctx, bumped, "compromised", "op", noAudit))
	register(t, s, bumped)
	require.NoError(t, s.Revoke(ctx, revoked, "compromised", "op", noAudit))

	entries, err := s.RevocationEntries(ctx)
	require.NoError(t, err)
	got := map[string]revocation.Entry{}
	for _, e := range entries {
		got[e.ID] = e
	}
	assert.NotContains(t, got, clean, "clean host is not in the snapshot set")
	assert.Equal(t, int64(1), got[bumped].Epoch)
	assert.False(t, got[bumped].Revoked)
	assert.True(t, got[revoked].Revoked)
}

func TestList_Get(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := t.Context()
	register(t, s, testUUID)

	rows, err := s.List(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, testUUID, rows[0].HostID)

	// No token material leaks through the JSON shape.
	buf, err := json.Marshal(rows)
	require.NoError(t, err)
	assert.NotContains(t, string(buf), "host_token")
	assert.NotContains(t, string(buf), "token_hash")

	e, err := s.Get(ctx, testUUID)
	require.NoError(t, err)
	assert.Equal(t, testUUID, e.HostID)

	_, err = s.Get(ctx, "00000000-0000-0000-0000-000000000000")
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestCountActive_ActiveHostIDs(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := t.Context()
	active := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	gone := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	register(t, s, active)
	register(t, s, gone)
	require.NoError(t, s.Revoke(ctx, gone, "x", "y", noAudit))

	n, err := s.CountActive(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	ids, err := s.ActiveHostIDs(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{active}, ids)
}

func TestRevoke_IdempotentAndUnknown(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := t.Context()
	register(t, s, testUUID)

	require.NoError(t, s.Revoke(ctx, testUUID, "compromised", "jane@customer.com", noAudit))
	_, revoked, err := s.TokenStatus(ctx, testUUID)
	require.NoError(t, err)
	assert.True(t, revoked)

	// Second revoke is idempotent and preserves the first actor/reason.
	before, err := s.Get(ctx, testUUID)
	require.NoError(t, err)
	require.NoError(t, s.Revoke(ctx, testUUID, "different-reason", "someoneElse", noAudit))
	after, err := s.Get(ctx, testUUID)
	require.NoError(t, err)
	assert.Equal(t, before.RevokeReason, after.RevokeReason)
	assert.Equal(t, before.RevokedBy, after.RevokedBy)

	assert.ErrorIs(t, s.Revoke(ctx, "00000000-0000-0000-0000-000000000000", "x", "y", noAudit), mysql.ErrNotFound)
}

// ActiveEnrollments reads only non-revoked enrollments, with the time each last enrolled. That time is on the database clock, the clock
// commands.created_at uses, which the watched-path catch-up compares it against (issue #998).
func TestActiveEnrollments(t *testing.T) {
	t.Parallel()
	s, db := newTestStoreWithDB(t)
	ctx := t.Context()
	register(t, s, "11111111-1111-4111-8111-111111111111")
	register(t, s, "22222222-2222-4222-8222-222222222222")
	require.NoError(t, s.Revoke(ctx, "22222222-2222-4222-8222-222222222222", "retired", "op", noAudit))

	got, err := s.ActiveEnrollments(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "11111111-1111-4111-8111-111111111111", got[0].HostID)

	var dbNow time.Time
	require.NoError(t, db.GetContext(ctx, &dbNow, `SELECT NOW(6)`))
	assert.WithinDuration(t, dbNow, got[0].EnrolledAt, 5*time.Second)

	// A re-enrollment moves the time forward.
	before := got[0].EnrolledAt
	time.Sleep(2 * time.Millisecond)
	register(t, s, "11111111-1111-4111-8111-111111111111")
	got, err = s.ActiveEnrollments(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, got[0].EnrolledAt.After(before), "re-enrolling must advance enrolled_at")
}
