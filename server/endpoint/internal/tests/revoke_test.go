//go:build integration

package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/endpoint/api"
	"github.com/rawtrail/rawtrail/server/endpoint/bootstrap"
	identityapi "github.com/rawtrail/rawtrail/server/identity/api"
)

// recordedAudit is an identityapi.AuditRecorder that keeps what it is handed. Safe for the sweep goroutine to write while the test
// reads.
type recordedAudit struct {
	mu     sync.Mutex
	events []identityapi.AuditEvent
}

func (r *recordedAudit) Record(_ context.Context, e identityapi.AuditEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	return nil
}

func (r *recordedAudit) recorded() []identityapi.AuditEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]identityapi.AuditEvent(nil), r.events...)
}

// spec:server-admin-surface/audit-trail-for-state-changing-admin-actions/revoke-produces-an-audit-record
// spec:agent-enrollment/revoke-ends-prior-credentials/revoke-audit-entry-commits-with-the-revoke
//
// TestRevoke_AuditRowDeliveredFromTheOutbox drives a revoke through the operator route as cmd/main mounts it, with the endpoint's audit
// sweep running. The revoke commits its entry to the outbox in its own transaction, and the sweep delivers it as one audit row that
// names the authenticated principal, the address the request came from, and the operator-supplied actor and reason.
func TestRevoke_AuditRowDeliveredFromTheOutbox(t *testing.T) {
	t.Parallel()
	audit := &recordedAudit{}
	ep := newEndpoint(t, func(d *bootstrap.Deps) {
		d.Audit = audit
		d.AuditSweepInterval = 50 * time.Millisecond
	})
	ctx := t.Context()
	_, err := ep.Service().Enroll(ctx, api.EnrollRequest{
		EnrollSecret: testEnrollSecret, HardwareUUID: testHardwareUUID, Hostname: "h", OSVersion: "macOS 15", AgentVersion: "0.1.0",
	}, "192.0.2.1")
	require.NoError(t, err)

	sweepCtx, stopSweep := context.WithCancel(ctx)
	sweepDone := make(chan struct{})
	go func() {
		defer close(sweepDone)
		ep.RunAuditSweep(sweepCtx)
	}()
	t.Cleanup(func() {
		stopSweep()
		<-sweepDone
	})

	principal := identityapi.PrincipalRef{ID: "usr_7", Type: identityapi.PrincipalUser, Label: "operator@example.com"}
	mux := http.NewServeMux()
	ep.RegisterAuthedRoutes(mux)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r.WithContext(identityapi.WithActor(r.Context(), &identityapi.Actor{Principal: principal})))
	}))
	t.Cleanup(srv.Close)

	body, err := json.Marshal(map[string]string{"actor": "oncall", "reason": "token copied off the host"})
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/api/enrollments/"+testHardwareUUID+"/revoke",
		bytes.NewReader(body))
	require.NoError(t, err)
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	require.Eventually(t, func() bool { return len(audit.recorded()) > 0 }, 5*time.Second, 20*time.Millisecond,
		"the sweep delivers the revoke's audit entry")
	events := audit.recorded()
	require.Len(t, events, 1)
	got := events[0]
	assert.Equal(t, identityapi.AuditEnrollmentRevoke, got.Action)
	assert.Equal(t, principal, got.Actor)
	assert.Equal(t, "host", got.TargetType)
	assert.Equal(t, testHardwareUUID, got.TargetID)
	assert.Equal(t, "127.0.0.1", got.RemoteAddr)
	assert.Equal(t, map[string]any{"actor": "oncall", "reason": "token copied off the host"}, got.Payload)
}

// spec:agent-enrollment/revoke-ends-prior-credentials/stolen-token-stays-rejected-after-re-enrollment
//
// TestRevoke_StolenTokenCannotRefreshAfterReEnroll is the end-to-end form of issue #1225: a token copied before a revoke is refused
// by the refresh route after the real agent re-enrolls, while the agent's new token refreshes. Before the fix the revoke left the epoch
// alone, the re-enroll cleared revoked_at, and the copied token refreshed indefinitely.
func TestRevoke_StolenTokenCannotRefreshAfterReEnroll(t *testing.T) {
	t.Parallel()
	ep := newEndpoint(t)
	ctx := t.Context()
	enroll := func() api.EnrollResponse {
		t.Helper()
		res, err := ep.Service().Enroll(ctx, api.EnrollRequest{
			EnrollSecret: testEnrollSecret, HardwareUUID: testHardwareUUID, Hostname: "h", OSVersion: "macOS 15", AgentVersion: "0.1.0",
		}, "192.0.2.1")
		require.NoError(t, err)
		return res
	}
	stolen := enroll()

	require.NoError(t, ep.Service().Revoke(ctx, api.RevokeRequest{HostID: testHardwareUUID, Reason: "compromised", Actor: "op"}))
	reEnrolled := enroll()
	require.NoError(t, ep.RevocationSnapshot().Refresh(ctx))

	srv := httptest.NewServer(refreshMux(ep))
	t.Cleanup(srv.Close)

	resp := postRefresh(t, ctx, srv, stolen.HostToken)
	resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "the token issued before the revoke stays refused after re-enrollment")

	resp = postRefresh(t, ctx, srv, reEnrolled.HostToken)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "the re-enrolled agent's token refreshes")
}
