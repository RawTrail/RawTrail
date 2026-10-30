package operator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identityapi "github.com/rawtrail/rawtrail/server/identity/api"
	"github.com/rawtrail/rawtrail/server/rules/api"
	"github.com/rawtrail/rawtrail/server/rules/internal/credentialguard"
	"github.com/rawtrail/rawtrail/server/rules/internal/hostpush"
)

const credentialGuardPath = "/api/v1/detection-config/credential-guard" //nolint:gosec // G101: a route, not a credential.

type fakeCredentialGuard struct {
	policy     api.CredentialGuardPolicy
	getErr     error
	replaceErr error
	gotMode    string
	gotCreds   []api.ProtectedCredential
	gotExpect  *int64
	replaced   bool
}

func (f *fakeCredentialGuard) Get(context.Context) (api.CredentialGuardPolicy, error) {
	return f.policy, f.getErr
}

func (f *fakeCredentialGuard) Replace(_ context.Context, actor *identityapi.Actor, _, mode string, credentials []api.ProtectedCredential,
	expectedVersion *int64) (credentialguard.ReplaceResult, error) {
	f.replaced, f.gotMode, f.gotCreds, f.gotExpect = true, mode, credentials, expectedVersion
	if f.replaceErr != nil {
		return credentialguard.ReplaceResult{}, f.replaceErr
	}
	policy := api.CredentialGuardPolicy{Version: 2, Mode: mode, Credentials: credentials, UpdatedBy: actor.Principal.ID}
	return credentialguard.ReplaceResult{Policy: policy, Counts: hostpush.Counts{FanoutHosts: 2}}, nil
}

var _ credentialGuardService = (*credentialguard.Service)(nil)

func credentialGuardServer(t *testing.T, svc credentialGuardService, az identityapi.AuthZ) *httptest.Server {
	t.Helper()
	h := NewDetectionConfig(&fakeDCService{}, az, slog.Default())
	h.SetCredentialGuard(svc)
	h.SetPrincipalLabelResolver(func(context.Context, string) (string, error) { return "ops@example.com", nil })
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := identityapi.WithActor(r.Context(), &identityapi.Actor{Principal: identityapi.UserPrincipal(7, "")})
		mux.ServeHTTP(w, r.WithContext(ctx))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCredentialGuardHandler_Get(t *testing.T) {
	t.Parallel()
	svc := &fakeCredentialGuard{policy: api.CredentialGuardPolicy{Version: 3, Mode: "off", Credentials: []api.ProtectedCredential{},
		UpdatedBy: "usr_7"}}
	resp := dcDo(t, credentialGuardServer(t, svc, allowAllAuthZ{}), http.MethodGet, credentialGuardPath, "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body api.CredentialGuardPolicy
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ops@example.com", body.UpdatedByLabel)
	assert.Equal(t, "off", body.Mode)

	failing := dcDo(t, credentialGuardServer(t, &fakeCredentialGuard{getErr: errors.New("database unavailable")}, allowAllAuthZ{}),
		http.MethodGet, credentialGuardPath, "")
	defer failing.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, failing.StatusCode)
}

func TestCredentialGuardHandler_ReplaceMapsEachOutcome(t *testing.T) {
	t.Parallel()
	body := `{"mode":"protect","credentials":[{"kind":"netrc","path":".netrc","match":"literal","owners":[]}],"reason":"r"}`
	cases := []struct {
		name       string
		body       string
		err        error
		wantStatus int
		wantInBody string
	}{
		{"stored", body, nil, http.StatusOK, `"fanout_hosts":2`},
		{"no credentials", `{"mode":"off","reason":"r"}`, nil, http.StatusBadRequest, "credentials is required"},
		{"no reason", body, credentialguard.ErrReasonRequired, http.StatusBadRequest, "reason is required"},
		{"invalid", body, credentialguard.InvalidPolicyError{Err: errors.New("credentials[0]: kind must be one of x")},
			http.StatusBadRequest, "kind must be one of"},
		{"changed since read", body, credentialguard.ErrVersionConflict, http.StatusConflict, "changed since it was read"},
		{"store failure", body, errors.New("database unavailable"), http.StatusInternalServerError, "internal error"},
		{"not JSON", `{`, nil, http.StatusBadRequest, "detection_config.invalid_json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := &fakeCredentialGuard{replaceErr: tc.err}
			resp := dcDo(t, credentialGuardServer(t, svc, allowAllAuthZ{}), http.MethodPut, credentialGuardPath, tc.body)
			defer resp.Body.Close()
			assert.Equal(t, tc.wantStatus, resp.StatusCode)
			raw := new(bytes.Buffer)
			_, _ = raw.ReadFrom(resp.Body)
			assert.Contains(t, raw.String(), tc.wantInBody)
			assert.NotContains(t, raw.String(), "database unavailable", "a server error must not leak its cause")
		})
	}

	t.Run("passes the policy and the expected version through", func(t *testing.T) {
		t.Parallel()
		svc := &fakeCredentialGuard{}
		resp := dcDo(t, credentialGuardServer(t, svc, allowAllAuthZ{}), http.MethodPut, credentialGuardPath,
			`{"mode":"off","credentials":[],"reason":"r","expected_version":4}`)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "off", svc.gotMode)
		assert.Equal(t, []api.ProtectedCredential{}, svc.gotCreds, "an empty list is a list, not an absent one")
		require.NotNil(t, svc.gotExpect)
		assert.Equal(t, int64(4), *svc.gotExpect)
	})
}

// spec:server-admin-surface/the-credential-guard-is-configured-over-the-api/reading-and-changing-it-need-detection-config-permissions
func TestCredentialGuardHandler_NeedsDetectionConfigPermissions(t *testing.T) {
	t.Parallel()
	svc := &fakeCredentialGuard{}
	srv := credentialGuardServer(t, svc, denyAllAuthZ{})
	get := dcDo(t, srv, http.MethodGet, credentialGuardPath, "")
	defer get.Body.Close()
	assert.Equal(t, http.StatusForbidden, get.StatusCode)
	put := dcDo(t, srv, http.MethodPut, credentialGuardPath, `{"mode":"off","credentials":[],"reason":"r"}`)
	defer put.Body.Close()
	assert.Equal(t, http.StatusForbidden, put.StatusCode)
	assert.False(t, svc.replaced, "nothing is changed without the grant")
}
