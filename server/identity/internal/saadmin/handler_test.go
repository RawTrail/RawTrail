package saadmin

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/identity/api"
	"github.com/rawtrail/rawtrail/server/identity/internal/serviceaccounts"
)

type fakeMgmtStore struct {
	list        []serviceaccounts.ServiceAccount
	created     serviceaccounts.ServiceAccount
	createInput serviceaccounts.CreateInput
	createCalls int
	rotateErr   error
	rotateCalls int
	rotateNow   time.Time
	rotateUntil time.Time
	revokeErr   error
	revokeCalls int
}

func (f *fakeMgmtStore) List(context.Context) ([]serviceaccounts.ServiceAccount, error) {
	return f.list, nil
}

func (f *fakeMgmtStore) Create(_ context.Context, in serviceaccounts.CreateInput) (serviceaccounts.ServiceAccount, string, error) {
	f.createCalls++
	f.createInput = in
	f.created = serviceaccounts.ServiceAccount{
		ID: 7, ClientID: "sa_created", Name: in.Name, RoleID: in.RoleID, ExpiresAt: in.ExpiresAt, CreatedAt: time.Unix(1_700_000_000, 0),
	}
	return f.created, "edrsa_one_time_secret", nil
}

func (f *fakeMgmtStore) Rotate(_ context.Context, id int64, now, expiresAt time.Time) (serviceaccounts.ServiceAccount, string, error) {
	f.rotateCalls++
	f.rotateNow, f.rotateUntil = now, expiresAt
	if f.rotateErr != nil {
		return serviceaccounts.ServiceAccount{}, "", f.rotateErr
	}
	return serviceaccounts.ServiceAccount{
		ID: id, ClientID: "sa_rotated", Name: "rotated", RoleID: "analyst", ExpiresAt: expiresAt, CreatedAt: time.Unix(1_600_000_000, 0),
	}, "edrsa_rotated_secret", nil
}

func (f *fakeMgmtStore) Revoke(context.Context, int64) error {
	f.revokeCalls++
	return f.revokeErr
}

type allowAuthZ struct{}

func (allowAuthZ) Allow(context.Context, api.Action, api.Resource) (api.Decision, error) {
	return api.Decision{Allow: true, Reason: "granted"}, nil
}

type denyAuthZ struct{}

func (denyAuthZ) Allow(context.Context, api.Action, api.Resource) (api.Decision, error) {
	return api.Decision{Allow: false, Reason: "no_matching_rule"}, nil
}

type captureAudit struct{ events []api.AuditEvent }

func (c *captureAudit) Record(_ context.Context, e api.AuditEvent) error {
	c.events = append(c.events, e)
	return nil
}

func newHandler(store ManagementStore, az api.AuthZ, audit AuditRecorder) *Handler {
	h := NewHandler(store, az, audit, nil)
	h.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	return h
}

func withActor(r *http.Request, userID int64) *http.Request {
	return r.WithContext(api.WithActor(r.Context(), &api.Actor{Principal: api.UserPrincipal(userID, ""), AuthMethod: "oidc"}))
}

func TestHandleList(t *testing.T) {
	t.Parallel()
	store := &fakeMgmtStore{list: []serviceaccounts.ServiceAccount{
		{ID: 1, ClientID: "sa_active", Name: "ci", RoleID: "analyst", ExpiresAt: time.Unix(1_700_000_000, 0).Add(time.Hour)},
		{ID: 2, ClientID: "sa_revoked", Name: "old", RoleID: "auditor", ExpiresAt: time.Unix(1_700_000_000, 0).Add(time.Hour), RevokedAt: sql.NullTime{Valid: true, Time: time.Unix(1, 0)}},
		{ID: 3, ClientID: "sa_expired", Name: "stale", RoleID: "analyst", ExpiresAt: time.Unix(1, 0)},
	}}
	h := newHandler(store, allowAuthZ{}, &captureAudit{})
	w := httptest.NewRecorder()
	h.handleList(w, withActor(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/settings/service-accounts", nil), 1))
	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		ServiceAccounts []saView `json:"service_accounts"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.ServiceAccounts, 3)
	assert.Equal(t, "active", resp.ServiceAccounts[0].Status)
	assert.Equal(t, "revoked", resp.ServiceAccounts[1].Status)
	assert.Equal(t, "expired", resp.ServiceAccounts[2].Status)
}

// spec:server-identity-authorization/service-account-management-actions-are-registered-and-admin-scoped/admin-holds-the-service-account-actions
func TestHandleList_deniedIsForbidden(t *testing.T) {
	t.Parallel()
	h := newHandler(&fakeMgmtStore{}, denyAuthZ{}, &captureAudit{})
	w := httptest.NewRecorder()
	h.handleList(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/settings/service-accounts", nil))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func createReq(t *testing.T, body any) *http.Request {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/settings/service-accounts", strings.NewReader(string(b)))
	return withActor(r, 42)
}

// spec:server-identity-service-accounts/service-account-lifecycle-and-token-issuance-are-audited/creating-a-service-account-writes-an-audit-row-without-the-secret
func TestHandleCreate_success(t *testing.T) {
	t.Parallel()
	store := &fakeMgmtStore{}
	audit := &captureAudit{}
	h := newHandler(store, allowAuthZ{}, audit)
	w := httptest.NewRecorder()
	h.handleCreate(w, createReq(t, map[string]any{"name": "ci-bot", "role": "analyst"}))
	require.Equal(t, http.StatusCreated, w.Code)

	var resp struct {
		saView
		Secret string `json:"secret"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "edrsa_one_time_secret", resp.Secret, "the one-time secret is returned on create")
	assert.Equal(t, "sa_created", resp.ClientID)
	assert.Equal(t, "analyst", store.createInput.RoleID)
	require.NotNil(t, store.createInput.CreatedBy)
	assert.Equal(t, "usr_42", *store.createInput.CreatedBy)
	// Default 90-day lifetime from the injected clock.
	assert.Equal(t, time.Unix(1_700_000_000, 0).Add(defaultLifetime).UTC(), store.createInput.ExpiresAt)

	require.Len(t, audit.events, 1)
	assert.Equal(t, api.AuditAction("service_account.created"), audit.events[0].Action)
	assert.NotContains(t, mustJSON(t, audit.events[0]), "secret", "audit row never carries the secret")
}

func TestHandleCreate_validation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		body   map[string]any
		reason string
	}{
		{"missing name", map[string]any{"name": " ", "role": "analyst"}, "missing_name"},
		{"super_admin rejected", map[string]any{"name": "x", "role": "super_admin"}, "invalid_role"},
		{"unknown role rejected", map[string]any{"name": "x", "role": "wizard"}, "invalid_role"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &fakeMgmtStore{}
			h := newHandler(store, allowAuthZ{}, &captureAudit{})
			w := httptest.NewRecorder()
			h.handleCreate(w, createReq(t, tc.body))
			require.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), tc.reason)
			assert.Equal(t, 0, store.createCalls, "an invalid request must not reach the store")
		})
	}
}

func TestHandleCreate_malformedBody(t *testing.T) {
	t.Parallel()
	t.Run("invalid json", func(t *testing.T) {
		t.Parallel()
		store := &fakeMgmtStore{}
		h := newHandler(store, allowAuthZ{}, &captureAudit{})
		r := withActor(httptest.NewRequestWithContext(t.Context(), http.MethodPost,
			"/api/settings/service-accounts", strings.NewReader("{not json")), 1)
		w := httptest.NewRecorder()
		h.handleCreate(w, r)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, 0, store.createCalls)
	})
	t.Run("oversized body", func(t *testing.T) {
		t.Parallel()
		store := &fakeMgmtStore{}
		h := newHandler(store, allowAuthZ{}, &captureAudit{})
		big := `{"name":"` + strings.Repeat("a", maxBodyBytes+10) + `"}`
		r := withActor(httptest.NewRequestWithContext(t.Context(), http.MethodPost,
			"/api/settings/service-accounts", strings.NewReader(big)), 1)
		w := httptest.NewRecorder()
		h.handleCreate(w, r)
		assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
		assert.Equal(t, 0, store.createCalls)
	})
}

func TestHandleCreate_nameTooLong(t *testing.T) {
	t.Parallel()
	store := &fakeMgmtStore{}
	h := newHandler(store, allowAuthZ{}, &captureAudit{})
	w := httptest.NewRecorder()
	h.handleCreate(w, createReq(t, map[string]any{"name": strings.Repeat("a", maxNameLen+1), "role": "analyst"}))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "name_too_long")
	assert.Equal(t, 0, store.createCalls)
}

func TestHandleCreate_nilAuditDoesNotPanic(t *testing.T) {
	t.Parallel()
	store := &fakeMgmtStore{}
	h := newHandler(store, allowAuthZ{}, nil)
	w := httptest.NewRecorder()
	h.handleCreate(w, createReq(t, map[string]any{"name": "x", "role": "analyst"}))
	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestHandleCreate_lifetimeCapped(t *testing.T) {
	t.Parallel()
	store := &fakeMgmtStore{}
	h := newHandler(store, allowAuthZ{}, &captureAudit{})
	w := httptest.NewRecorder()
	days := 1000 // beyond the 365-day max
	h.handleCreate(w, createReq(t, map[string]any{"name": "x", "role": "auditor", "expires_in_days": days}))
	require.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, time.Unix(1_700_000_000, 0).Add(maxLifetime).UTC(), store.createInput.ExpiresAt, "lifetime is capped at the max")
}

func TestHandleCreate_deniedIsForbidden(t *testing.T) {
	t.Parallel()
	store := &fakeMgmtStore{}
	h := newHandler(store, denyAuthZ{}, &captureAudit{})
	w := httptest.NewRecorder()
	h.handleCreate(w, createReq(t, map[string]any{"name": "x", "role": "analyst"}))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, 0, store.createCalls)
}

func TestHandleRotate(t *testing.T) {
	t.Parallel()
	t.Run("success", func(t *testing.T) {
		t.Parallel()
		store := &fakeMgmtStore{}
		audit := &captureAudit{}
		h := newHandler(store, allowAuthZ{}, audit)
		w := httptest.NewRecorder()
		r := withActor(httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/settings/service-accounts/5/rotate", nil), 1)
		r.SetPathValue("id", "5")
		h.handleRotate(w, r)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "edrsa_rotated_secret")
		require.Len(t, audit.events, 1)
		assert.Equal(t, api.AuditAction("service_account.rotated"), audit.events[0].Action)
	})
	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		store := &fakeMgmtStore{rotateErr: serviceaccounts.ErrNotFound}
		h := newHandler(store, allowAuthZ{}, &captureAudit{})
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/settings/service-accounts/9/rotate", nil)
		r.SetPathValue("id", "9")
		h.handleRotate(w, r)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
	// spec:server-identity-service-accounts/rotating-a-service-account-resets-its-lifetime/a-revoked-or-expired-account-is-not-rotated
	t.Run("a revoked or expired account is a conflict, and is not audited as rotated", func(t *testing.T) {
		t.Parallel()
		store := &fakeMgmtStore{rotateErr: serviceaccounts.ErrNotRotatable}
		audit := &captureAudit{}
		h := newHandler(store, allowAuthZ{}, audit)
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/settings/service-accounts/9/rotate", nil)
		r.SetPathValue("id", "9")
		h.handleRotate(w, r)
		assert.Equal(t, http.StatusConflict, w.Code)
		assert.JSONEq(t, `{"error":"not_rotatable"}`, w.Body.String())
		assert.NotContains(t, w.Body.String(), "secret")
		assert.Empty(t, audit.events)
	})
	t.Run("invalid id", func(t *testing.T) {
		t.Parallel()
		h := newHandler(&fakeMgmtStore{}, allowAuthZ{}, &captureAudit{})
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/settings/service-accounts/abc/rotate", nil)
		r.SetPathValue("id", "abc")
		h.handleRotate(w, r)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// The lifetime a rotate asks for is resolved by the same rule as create's: a default of 90 days, a requested count honoured, and 365
// as the cap, all counted from now rather than from the account's old deadline.
//
// spec:server-identity-service-accounts/rotating-a-service-account-resets-its-lifetime/rotation-sets-a-fresh-expiry
// spec:server-identity-service-accounts/rotating-a-service-account-resets-its-lifetime/a-requested-lifetime-is-honoured-and-capped
func TestHandleRotate_Lifetime(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	for _, tc := range []struct {
		name string
		body string
		want time.Duration
	}{
		{"no body gets the default lifetime", "", defaultLifetime},
		{"an empty object gets the default lifetime", `{}`, defaultLifetime},
		{"a requested lifetime is honoured", `{"expires_in_days":30}`, 30 * day},
		{"a request above the cap is capped", `{"expires_in_days":4000}`, maxLifetime},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &fakeMgmtStore{}
			h := newHandler(store, allowAuthZ{}, &captureAudit{})
			w := httptest.NewRecorder()
			var body io.Reader
			if tc.body != "" {
				body = strings.NewReader(tc.body)
			}
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/settings/service-accounts/5/rotate", body)
			r.SetPathValue("id", "5")
			h.handleRotate(w, r)

			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, now, store.rotateNow)
			assert.Equal(t, now.Add(tc.want).UTC(), store.rotateUntil)
			var resp struct {
				Secret    string    `json:"secret"`
				ExpiresAt time.Time `json:"expires_at"`
				ClientID  string    `json:"client_id"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, "edrsa_rotated_secret", resp.Secret)
			assert.True(t, resp.ExpiresAt.Equal(now.Add(tc.want)), "the response carries the new expiry")
			assert.Equal(t, "sa_rotated", resp.ClientID)
		})
	}

	// A chunked request with nothing in it reports no length, so judging the body absent from ContentLength alone refuses it.
	t.Run("an empty chunked body is no body", func(t *testing.T) {
		t.Parallel()
		store := &fakeMgmtStore{}
		h := newHandler(store, allowAuthZ{}, &captureAudit{})
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/settings/service-accounts/5/rotate", strings.NewReader(""))
		r.ContentLength = -1
		r.SetPathValue("id", "5")
		h.handleRotate(w, r)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, time.Unix(1_700_000_000, 0).Add(defaultLifetime).UTC(), store.rotateUntil)
	})

	t.Run("a malformed body is a bad request and rotates nothing", func(t *testing.T) {
		t.Parallel()
		store := &fakeMgmtStore{}
		h := newHandler(store, allowAuthZ{}, &captureAudit{})
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/settings/service-accounts/5/rotate", strings.NewReader("{nope"))
		r.SetPathValue("id", "5")
		h.handleRotate(w, r)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Zero(t, store.rotateCalls)
	})
}

func TestHandleRevoke(t *testing.T) {
	t.Parallel()
	t.Run("success", func(t *testing.T) {
		t.Parallel()
		store := &fakeMgmtStore{}
		audit := &captureAudit{}
		h := newHandler(store, allowAuthZ{}, audit)
		w := httptest.NewRecorder()
		r := withActor(httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/api/settings/service-accounts/5", nil), 1)
		r.SetPathValue("id", "5")
		h.handleRevoke(w, r)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 1, store.revokeCalls)
		require.Len(t, audit.events, 1)
		assert.Equal(t, api.AuditAction("service_account.revoked"), audit.events[0].Action)
	})
	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		store := &fakeMgmtStore{revokeErr: serviceaccounts.ErrNotFound}
		h := newHandler(store, allowAuthZ{}, &captureAudit{})
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/api/settings/service-accounts/9", nil)
		r.SetPathValue("id", "9")
		h.handleRevoke(w, r)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}
