package service_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fleetdm/edr/server/auditoutbox"
	"github.com/fleetdm/edr/server/endpoint/api"
	"github.com/fleetdm/edr/server/endpoint/internal/mysql"
	"github.com/fleetdm/edr/server/endpoint/internal/revocation"
	"github.com/fleetdm/edr/server/endpoint/internal/service"
	"github.com/fleetdm/edr/server/endpoint/internal/signedtoken"
	"github.com/fleetdm/edr/server/endpoint/testkit"
	"github.com/fleetdm/edr/server/testdb"
)

// noRevocations is a do-nothing revocation.Source for constructing a snapshot in the New-guard test without a database.
type noRevocations struct{}

func (noRevocations) RevocationEntries(context.Context) ([]revocation.Entry, error) { return nil, nil }

// TestServiceNew_Panics covers the fail-fast guards: New panics on a missing Store, Signer, Revocations, AuditOutbox, or empty Secret,
// and does not panic when all are present.
func TestServiceNew_Panics(t *testing.T) {
	t.Parallel()
	store := &mysql.Store{}
	signer, err := signedtoken.New(testSigningKey, "v1")
	require.NoError(t, err)
	snap := revocation.NewSnapshot(noRevocations{}, nil)
	outbox := auditoutbox.NewStore(&sqlx.DB{}, mysql.AuditOutboxTable)

	assert.Panics(t, func() { service.New(service.Options{}) }, "nil store")
	assert.Panics(t, func() { service.New(service.Options{Store: store}) }, "nil signer")
	assert.Panics(t, func() { service.New(service.Options{Store: store, Signer: signer}) }, "nil revocations")
	assert.Panics(t, func() { service.New(service.Options{Store: store, Signer: signer, Revocations: snap}) }, "empty secret")
	assert.Panics(t, func() { service.New(service.Options{Store: store, Signer: signer, Revocations: snap, Secret: "s"}) },
		"nil audit outbox")
	assert.NotPanics(t, func() {
		service.New(service.Options{Store: store, Signer: signer, Revocations: snap, Secret: "s", AuditOutbox: outbox})
	})
}

const (
	testHostID = "93DFC6F5-763D-5075-B305-8AC145D12F96"
	testSecret = "test-enroll-secret"
)

// Fixed >=32-byte keys so a token minted at enroll verifies on a later call within the same test.
var testSigningKey = []byte("test-host-token-signing-0123456789abcdef")

// newServiceForTest stands up an endpoint Service over a real MySQL test DB, wired with the signer + revocation snapshot the
// self-validating-token model needs. The snapshot is returned so tests can Refresh it deterministically (production refreshes it on a
// ticker; tests drive it by hand to avoid sleeping).
func newServiceForTest(t *testing.T) (api.Service, *revocation.Snapshot) {
	t.Helper()
	db := testdb.Open(t)
	require.NoError(t, testkit.ApplySchema(t.Context(), db))
	store := mysql.NewStore(db)
	signer, err := signedtoken.New(testSigningKey, "v1")
	require.NoError(t, err)
	snap := revocation.NewSnapshot(store, slog.Default())
	svc := service.New(service.Options{
		Store:       store,
		Secret:      testSecret,
		Signer:      signer,
		Revocations: snap,
		AuditOutbox: auditoutbox.NewStore(db, mysql.AuditOutboxTable),
		TokenTTL:    time.Hour,
		Logger:      slog.Default(),
	})
	return svc, snap
}

func enrollForTest(t *testing.T, svc api.Service) api.EnrollResponse {
	t.Helper()
	res, err := svc.Enroll(t.Context(), api.EnrollRequest{
		EnrollSecret: testSecret,
		HardwareUUID: testHostID,
		Hostname:     "h",
		OSVersion:    "macOS 14",
		AgentVersion: "0.1.0",
	}, "192.0.2.1")
	require.NoError(t, err)
	return res
}

// spec:agent-enrollment/host-tokens-are-self-validating-signed-tokens/issued-token-verifies-without-a-database-lookup
//
// TestEnroll_MintsVerifiableSignedToken: the enroll response carries a self-validating token that verifies, with an expiry about one
// TTL out.
func TestEnroll_MintsVerifiableSignedToken(t *testing.T) {
	t.Parallel()
	svc, _ := newServiceForTest(t)
	res := enrollForTest(t, svc)
	require.NotEmpty(t, res.HostToken)
	assert.WithinDuration(t, time.Now().Add(time.Hour), res.ExpiresAt, 2*time.Minute)

	hostID, err := svc.VerifyToken(t.Context(), res.HostToken)
	require.NoError(t, err)
	assert.Equal(t, testHostID, hostID)
}

// TestRefreshToken_IssuesFreshVerifiableToken: with host_id pinned on the context (as the middleware does), refresh returns a new,
// distinct, verifiable token.
func TestRefreshToken_IssuesFreshVerifiableToken(t *testing.T) {
	t.Parallel()
	svc, _ := newServiceForTest(t)
	res := enrollForTest(t, svc)

	ref, err := svc.RefreshToken(t.Context(), res.HostToken)
	require.NoError(t, err)
	require.NotEmpty(t, ref.HostToken)
	assert.Equal(t, testHostID, ref.HostID)

	hostID, err := svc.VerifyToken(t.Context(), ref.HostToken)
	require.NoError(t, err)
	assert.Equal(t, testHostID, hostID)
}

// revokeForTest revokes testHostID as an operator would.
func revokeForTest(t *testing.T, svc api.Service) {
	t.Helper()
	require.NoError(t, svc.Revoke(t.Context(), api.RevokeRequest{HostID: testHostID, Reason: "incident", Actor: "op"}))
}

// TestRefreshToken_RejectsStaleEpochAndGarbage covers the refresh-path guards: an unverifiable token is rejected, and (the security
// fix) a token issued before a revoke cannot refresh after the host re-enrolls, even though the revocation snapshot here is never
// refreshed (the refresh path checks the DB epoch directly, closing the snapshot-staleness window). Before issue #1225 the revoke left
// the epoch alone, so the re-enroll that cleared revoked_at made the stolen token refreshable again.
func TestRefreshToken_RejectsStaleEpochAndGarbage(t *testing.T) {
	t.Parallel()
	svc, _ := newServiceForTest(t)
	res := enrollForTest(t, svc)

	_, err := svc.RefreshToken(t.Context(), "not.a.token")
	require.ErrorIs(t, err, api.ErrInvalidToken)

	revokeForTest(t, svc)
	enrollForTest(t, svc)
	_, err = svc.RefreshToken(t.Context(), res.HostToken)
	require.ErrorIs(t, err, api.ErrInvalidToken, "a token issued before the revoke must not refresh after the host re-enrolls")
}

// spec:agent-enrollment/revocation-is-enforced-by-a-per-replica-snapshot/operator-rotate-invalidates-after-the-snapshot-refreshes
// spec:agent-enrollment/revoke-ends-prior-credentials/stolen-token-stays-rejected-after-re-enrollment
//
// TestRevoke_StolenTokenStaysRejectedAfterReEnroll: a revoke bumps the epoch, so after the snapshot refreshes the pre-revoke token is
// rejected, a re-enroll mints a fresh token that verifies, and the pre-revoke token stays rejected after that re-enroll.
func TestRevoke_StolenTokenStaysRejectedAfterReEnroll(t *testing.T) {
	t.Parallel()
	svc, snap := newServiceForTest(t)
	stolen := enrollForTest(t, svc)
	require.NoError(t, snap.Refresh(t.Context()))
	_, err := svc.VerifyToken(t.Context(), stolen.HostToken)
	require.NoError(t, err)

	revokeForTest(t, svc)
	require.NoError(t, snap.Refresh(t.Context()))
	_, err = svc.VerifyToken(t.Context(), stolen.HostToken)
	require.ErrorIs(t, err, api.ErrInvalidToken, "pre-revoke token rejected once the revoke is visible")

	// Re-enroll clears the revocation and PRESERVES the bumped epoch, so the fresh token verifies while the pre-revoke token stays
	// rejected. If the revoke had not bumped the epoch, the stolen token would be valid again here.
	reEnrolled := enrollForTest(t, svc)
	require.NoError(t, snap.Refresh(t.Context()))
	hostID, err := svc.VerifyToken(t.Context(), reEnrolled.HostToken)
	require.NoError(t, err, "re-enroll mints a fresh token at the preserved epoch")
	assert.Equal(t, testHostID, hostID)

	_, err = svc.VerifyToken(t.Context(), stolen.HostToken)
	require.ErrorIs(t, err, api.ErrInvalidToken, "the pre-revoke token MUST stay rejected after re-enroll")
}

// TestRevoke_NotFound: revoking a host with no enrollment is ErrNotFound.
func TestRevoke_NotFound(t *testing.T) {
	t.Parallel()
	svc, _ := newServiceForTest(t)
	err := svc.Revoke(t.Context(), api.RevokeRequest{HostID: "00000000-0000-0000-0000-000000000000", Reason: "a", Actor: "b"})
	require.ErrorIs(t, err, api.ErrNotFound)
}

// TestRevoke_InvalidatesAfterSnapshot: Revoke sets revoked_at; after the snapshot refreshes the token is rejected.
func TestRevoke_InvalidatesAfterSnapshot(t *testing.T) {
	t.Parallel()
	svc, snap := newServiceForTest(t)
	res := enrollForTest(t, svc)
	revokeForTest(t, svc)
	require.NoError(t, snap.Refresh(t.Context()))
	_, err := svc.VerifyToken(t.Context(), res.HostToken)
	require.ErrorIs(t, err, api.ErrInvalidToken)
}

// TestReEnroll_ClearsStaleSnapshotLocally: after a revoke leaves the snapshot rejecting the old token, a re-enroll yields a token that
// verifies immediately on this replica WITHOUT a snapshot refresh, because Enroll records the host's post-enrollment state in the local
// snapshot (the transient-401-after-re-enroll mitigation).
func TestReEnroll_ClearsStaleSnapshotLocally(t *testing.T) {
	t.Parallel()
	svc, snap := newServiceForTest(t)
	res := enrollForTest(t, svc)
	require.NoError(t, snap.Refresh(t.Context()))

	revokeForTest(t, svc)
	require.NoError(t, snap.Refresh(t.Context()))
	_, err := svc.VerifyToken(t.Context(), res.HostToken)
	require.ErrorIs(t, err, api.ErrInvalidToken, "old token rejected after the revoke")

	// Re-enroll, then verify with NO intervening snap.Refresh: the new token must pass because Enroll updated the host locally.
	reEnrolled := enrollForTest(t, svc)
	hostID, err := svc.VerifyToken(t.Context(), reEnrolled.HostToken)
	require.NoError(t, err, "re-enrolled token verifies immediately on the enrolling replica")
	assert.Equal(t, testHostID, hostID)
}
