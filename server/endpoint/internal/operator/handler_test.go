package operator

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fleetdm/edr/server/endpoint/api"
	identityapi "github.com/fleetdm/edr/server/identity/api"
)

// fakeRevokeService stubs api.Service. Only Revoke + List are exercised by these tests; other methods panic so a regression that wires
// this fake into a different path surfaces immediately.
type fakeRevokeService struct {
	revoke func(ctx context.Context, req api.RevokeRequest) error
}

func (f fakeRevokeService) Enroll(context.Context, api.EnrollRequest, string) (api.EnrollResponse, error) {
	panic("not used")
}
func (f fakeRevokeService) VerifyToken(context.Context, string) (string, error) { panic("not used") }
func (f fakeRevokeService) RefreshToken(context.Context, string) (api.RefreshResponse, error) {
	panic("not used")
}
func (f fakeRevokeService) List(context.Context) ([]api.Enrollment, error) { return nil, nil }
func (f fakeRevokeService) Get(context.Context, string) (*api.Enrollment, error) {
	panic("not used")
}
func (f fakeRevokeService) Revoke(ctx context.Context, req api.RevokeRequest) error {
	if f.revoke == nil {
		panic("fake.Revoke not set")
	}
	return f.revoke(ctx, req)
}
func (f fakeRevokeService) CountActive(context.Context) (int, error)        { panic("not used") }
func (f fakeRevokeService) ActiveHostIDs(context.Context) ([]string, error) { panic("not used") }
func (f fakeRevokeService) RecordStatus(context.Context, string, api.StatusReport) error {
	panic("not used")
}
func (f fakeRevokeService) ActiveEnrollments(context.Context) ([]api.ActiveEnrollment, error) {
	panic("not used")
}

// allowAllAuthZ stubs identityapi.AuthZ for endpoint operator tests that exercise revoke semantics rather than the role matrix.
// Per-action coverage lives in server/identity/internal/authz/engine_test.go.
type allowAllAuthZ struct{}

func (allowAllAuthZ) Allow(context.Context, identityapi.Action, identityapi.Resource) (identityapi.Decision, error) {
	return identityapi.Decision{Allow: true, Reason: "granted"}, nil
}

// postRevoke sends a revoke for hostID with body to a handler over svc and returns the status code.
func postRevoke(t *testing.T, svc api.Service, hostID string, body map[string]any) int {
	t.Helper()
	mux := http.NewServeMux()
	New(svc, allowAllAuthZ{}, nil).RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/api/enrollments/"+hostID+"/revoke",
		bytes.NewReader(encoded))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	return resp.StatusCode
}

// A successful revoke returns 204 and hands the service the host, the trimmed operator attribution, and the address the request came
// from, which the service records in the audit entry it commits with the revoke.
func TestHandler_Revoke_PassesTheRequestToTheService(t *testing.T) {
	t.Parallel()
	var got api.RevokeRequest
	svc := fakeRevokeService{revoke: func(_ context.Context, req api.RevokeRequest) error {
		got = req
		return nil
	}}

	status := postRevoke(t, svc, "H-9", map[string]any{"actor": " operator@test ", "reason": "compromise"})

	assert.Equal(t, http.StatusNoContent, status)
	assert.Equal(t, api.RevokeRequest{HostID: "H-9", Reason: "compromise", Actor: "operator@test", RemoteAddr: "127.0.0.1"}, got)
}

// spec:server-admin-surface/revoke-a-host-enrollment/revoke-without-an-actor-or-reason
//
// The revoke endpoint MUST refuse a request body missing `actor` or `reason` (or carrying whitespace-only values) with a 400 and MUST
// NOT call the service-level Revoke. The fake's t.Fatal proves the handler stops at validation rather than persisting the empty
// attribution.
func TestHandler_Revoke_RequiresActorAndReason(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing actor", map[string]any{"reason": "x"}},
		{"missing reason", map[string]any{"actor": "x"}},
		{"both empty", map[string]any{}},
		{"whitespace actor", map[string]any{"actor": "   ", "reason": "y"}},
		{"whitespace reason", map[string]any{"actor": "y", "reason": "\t"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := fakeRevokeService{revoke: func(context.Context, api.RevokeRequest) error {
				t.Fatal("Revoke must not be called when actor/reason validation fails")
				return nil
			}}
			assert.Equal(t, http.StatusBadRequest, postRevoke(t, svc, "H-1", tc.body))
		})
	}
}

// An unknown host is a 404, not a 500: the handler surfaces api.ErrNotFound from the service as the operator-facing "not_found" code.
func TestHandler_Revoke_NotFound(t *testing.T) {
	t.Parallel()
	svc := fakeRevokeService{revoke: func(context.Context, api.RevokeRequest) error { return api.ErrNotFound }}
	assert.Equal(t, http.StatusNotFound, postRevoke(t, svc, "missing", map[string]any{"actor": "op", "reason": "x"}))
}
