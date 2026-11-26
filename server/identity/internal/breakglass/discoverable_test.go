//go:build integration

package breakglass_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/identity/api"
	"github.com/rawtrail/rawtrail/server/identity/internal/breakglass"
	"github.com/rawtrail/rawtrail/server/identity/internal/users"
)

// A laptop owner's passkey sign-in with no email: the challenge names no account, and the account is the one the passkey's user
// handle names.

const testSigningKey = "0123456789abcdef0123456789abcdef"

// spec:server-identity-authentication/a-laptop-s-passkey-sign-in-can-name-no-account/a-discoverable-passkey-signs-its-owner-in
func TestService_FinishPasskeyLogin_SignsInTheAccountTheUserHandleNames(t *testing.T) {
	t.Parallel()
	svc, db, _, uid, fake := newFakeService(t)
	registerFakeCredential(t, svc, db, uid, fake)

	challenge, named, err := svc.BeginPasskeyLogin(t.Context(), "")
	require.NoError(t, err)
	assert.Nil(t, named, "the challenge names no account")
	assert.True(t, breakglass.NamesNoAccount(challenge.SessionData))
	assert.Equal(t, protocol.VerificationRequired, challenge.SessionData.UserVerification)

	owner, err := svc.PasskeyUser(t.Context(), userHandle(uid))
	require.NoError(t, err)
	sess, err := svc.FinishPasskeyLogin(t.Context(), breakglass.FinishLoginRequest{
		User: owner, Session: challenge.SessionData,
		Assertion: &protocol.ParsedCredentialAssertionData{Response: protocol.ParsedAssertionResponse{UserHandle: userHandle(uid)}},
	})
	require.NoError(t, err)
	assert.Equal(t, uid, sess.UserID)
	assert.Equal(t, 1, fake.discoverableValidations, "validated as an assertion for a challenge that named no account")
	rows, err := breakglass.NewCredentialStore(db).ListByUserID(t.Context(), uid)
	require.NoError(t, err)
	assert.Equal(t, uint64(fake.loginCredential.Authenticator.SignCount), rows[0].SignCount, "the sign count is recorded")
}

// spec:server-identity-authentication/a-laptop-s-passkey-sign-in-can-name-no-account/a-handle-outside-break-glass-is-refused
func TestService_PasskeyUser(t *testing.T) {
	t.Parallel()
	svc, db, _, uid, _ := newFakeService(t)
	sso, err := users.New(db).Create(t.Context(), users.CreateRequest{Email: "sso@example.com", Password: "not-used-here"})
	require.NoError(t, err)
	for _, tc := range []struct {
		name    string
		handle  []byte
		wantErr error
	}{
		{"a break-glass account", userHandle(uid), nil},
		{"an account outside break-glass", userHandle(sso.ID), breakglass.ErrNotBreakglass},
		{"no account", userHandle(sso.ID + 1000), users.ErrNotFound},
		{"a handle of another length", []byte("short"), users.ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := svc.PasskeyUser(t.Context(), tc.handle)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, uid, got.ID)
		})
	}
}

// spec:server-identity-authentication/a-laptop-s-passkey-sign-in-can-name-no-account/a-discoverable-passkey-signs-its-owner-in
//
// Through the laptop's routes: a challenge requested with no email names no account and requires user verification, and an assertion
// that carries the owner's user handle signs them in, with no email or password in the body.
func TestHandle_PasskeyLogin_WithNoEmailSignsInThePasskeysOwner(t *testing.T) {
	t.Parallel()
	srv, svc, db, rec, uid, fake := newPasskeyServer(t, true)
	registerFakeCredential(t, svc, db, uid, fake)

	challenge := postJSON(t, srv, "/admin/break-glass/passkey/challenge", `{}`, nil)
	require.Equal(t, http.StatusOK, challenge.StatusCode, "reason=%s", challenge.Header.Get("X-Edr-Auth-Reason"))
	state := cookieNamed(challenge, breakglass.ChallengeStateCookieName)
	require.NotNil(t, state)
	sd, err := breakglass.DecodeChallengeState([]byte(testSigningKey), state.Value)
	require.NoError(t, err)
	assert.True(t, breakglass.NamesNoAccount(sd))
	assert.Equal(t, protocol.VerificationRequired, sd.UserVerification)

	finish := postJSON(t, srv, "/admin/break-glass/passkey", assertionBody(t, map[string]any{}, userHandle(uid)), state)
	require.Equal(t, http.StatusOK, finish.StatusCode, "reason=%s", finish.Header.Get("X-Edr-Auth-Reason"))
	require.NotNil(t, cookieNamed(finish, api.SessionCookieName))
	last := rec.events[len(rec.events)-1]
	assert.Equal(t, api.AuditAuthBreakglassSuccess, last.Action)
	assert.Equal(t, strconv.FormatInt(uid, 10), last.TargetID)
	assert.Equal(t, "passkey_only", last.Payload["method"])
}

// spec:server-identity-authentication/a-laptop-s-passkey-sign-in-can-name-no-account/a-handle-outside-break-glass-is-refused
func TestHandle_PasskeyLogin_RefusesAUserHandleThatNamesNoBreakGlassAccount(t *testing.T) {
	t.Parallel()
	srv, svc, db, rec, uid, fake := newPasskeyServer(t, true)
	registerFakeCredential(t, svc, db, uid, fake)
	sso, err := users.New(db).Create(t.Context(), users.CreateRequest{Email: "sso@example.com", Password: "not-used-here"})
	require.NoError(t, err)
	for _, tc := range []struct {
		name   string
		handle []byte
		want   string
	}{
		{"an account outside break-glass", userHandle(sso.ID), "not_breakglass"},
		{"no account", userHandle(sso.ID + 1000), "user_not_found"},
	} {
		challenge := postJSON(t, srv, "/admin/break-glass/passkey/challenge", `{}`, nil)
		finish := postJSON(t, srv, "/admin/break-glass/passkey", assertionBody(t, map[string]any{}, tc.handle),
			cookieNamed(challenge, breakglass.ChallengeStateCookieName))
		assert.Equal(t, http.StatusUnauthorized, finish.StatusCode, tc.name)
		assert.Nil(t, cookieNamed(finish, api.SessionCookieName), tc.name)
		last := rec.events[len(rec.events)-1]
		assert.Equal(t, api.AuditAuthBreakglassFailure, last.Action, tc.name)
		assert.Equal(t, tc.want, last.Payload["reason"], tc.name)
	}
}

// The password sign-in names its account by email only: given a challenge that named no account and an assertion carrying the owner's
// user handle, with the right password and no email, it finds no account.
func TestHandle_PasswordLogin_DoesNotNameItsAccountByUserHandle(t *testing.T) {
	t.Parallel()
	srv, svc, db, rec, uid, fake := newPasskeyServer(t, true)
	registerFakeCredential(t, svc, db, uid, fake)
	challenge := postJSON(t, srv, "/admin/break-glass/passkey/challenge", `{}`, nil)
	finish := postJSON(t, srv, "/admin/break-glass", assertionBody(t, map[string]any{"password": "long-enough-password"}, userHandle(uid)),
		cookieNamed(challenge, breakglass.ChallengeStateCookieName))
	assert.Equal(t, http.StatusUnauthorized, finish.StatusCode)
	assert.Nil(t, cookieNamed(finish, api.SessionCookieName))
	assert.Equal(t, "user_not_found", rec.events[len(rec.events)-1].Payload["reason"])
}

// A sign-in with no email spends the per-email budget of the account the user handle names, whatever email the body carries: with a
// budget of one, the owner's second sign-in is refused even when the body names someone else.
func TestHandle_PasskeyLogin_WithNoEmailSpendsTheHandlesAccountBudget(t *testing.T) {
	t.Parallel()
	svc, db, rec, uid, fake := newFakeService(t)
	registerFakeCredential(t, svc, db, uid, fake)
	h := breakglass.NewHandler(breakglass.HandlerOptions{
		Service: svc, SigningKey: []byte(testSigningKey), Logger: slog.Default(), PasskeyOnly: true,
		RateLimits: breakglass.NewRateLimits(0, 1, 0),
	})
	mux := http.NewServeMux()
	h.RegisterPublicRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	signIn := func(fields map[string]any) *http.Response {
		challenge := postJSON(t, srv, "/admin/break-glass/passkey/challenge", `{}`, nil)
		return postJSON(t, srv, "/admin/break-glass/passkey", assertionBody(t, fields, userHandle(uid)),
			cookieNamed(challenge, breakglass.ChallengeStateCookieName))
	}
	first := signIn(map[string]any{})
	require.Equal(t, http.StatusOK, first.StatusCode, "reason=%s", first.Header.Get("X-Edr-Auth-Reason"))
	second := signIn(map[string]any{"email": "someone-else@example.com"})
	assert.Equal(t, http.StatusTooManyRequests, second.StatusCode)
	last := rec.events[len(rec.events)-1]
	assert.Equal(t, "email_rate_limited", last.Payload["reason"])
	assert.Equal(t, "admin@rawtrail.local", last.Actor.Label)
}

// A sign-in that names its account by email is refused for an account outside break-glass, and once that email's budget is spent. The
// passkey-only route shares the password sign-in's resolution, so this holds for both.
func TestHandle_PasskeyLogin_RefusesByEmail(t *testing.T) {
	t.Parallel()
	svc, db, rec, uid, fake := newFakeService(t)
	registerFakeCredential(t, svc, db, uid, fake)
	_, err := users.New(db).Create(t.Context(), users.CreateRequest{Email: "sso@example.com", Password: "not-used-here"})
	require.NoError(t, err)
	h := breakglass.NewHandler(breakglass.HandlerOptions{
		Service: svc, SigningKey: []byte(testSigningKey), Logger: slog.Default(), PasskeyOnly: true,
		RateLimits: breakglass.NewRateLimits(0, 1, 0),
	})
	mux := http.NewServeMux()
	h.RegisterPublicRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	challenge := postJSON(t, srv, "/admin/break-glass/passkey/challenge", `{"email":"admin@rawtrail.local"}`, nil)
	state := cookieNamed(challenge, breakglass.ChallengeStateCookieName)

	for _, tc := range []struct {
		name, email, want string
		status            int
	}{
		{"an account outside break-glass", "sso@example.com", "not_breakglass", http.StatusUnauthorized},
		{"an email whose budget is spent", "sso@example.com", "email_rate_limited", http.StatusTooManyRequests},
	} {
		resp := postJSON(t, srv, "/admin/break-glass/passkey", assertionBody(t, map[string]any{"email": tc.email}, nil), state)
		assert.Equal(t, tc.status, resp.StatusCode, tc.name)
		assert.Equal(t, tc.want, rec.events[len(rec.events)-1].Payload["reason"], tc.name)
	}
}

// With no email, an assertion that does not parse is refused before any account is looked up.
func TestHandle_PasskeyLogin_WithNoEmailRefusesAnUnparseableAssertion(t *testing.T) {
	t.Parallel()
	srv, _, _, rec, _, _ := newPasskeyServer(t, true)
	challenge := postJSON(t, srv, "/admin/break-glass/passkey/challenge", `{}`, nil)
	resp := postJSON(t, srv, "/admin/break-glass/passkey", `{"assertion":{}}`, cookieNamed(challenge, breakglass.ChallengeStateCookieName))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "assertion_parse_failed", rec.events[len(rec.events)-1].Payload["reason"])
}

// The engine's refusals surface from a sign-in with no email: a challenge it cannot issue, and an assertion it rejects, which records
// nothing.
func TestService_PasskeyLoginWithNoEmail_SurfacesEngineErrors(t *testing.T) {
	t.Parallel()
	svc, db, _, uid, fake := newFakeService(t)
	registerFakeCredential(t, svc, db, uid, fake)

	fake.beginLoginErr = protocol.ErrBadRequest
	_, _, err := svc.BeginPasskeyLogin(t.Context(), "")
	require.ErrorIs(t, err, protocol.ErrBadRequest)

	fake.beginLoginErr = nil
	fake.validateLoginErr = protocol.ErrVerification
	challenge, _, err := svc.BeginPasskeyLogin(t.Context(), "")
	require.NoError(t, err)
	owner, err := svc.PasskeyUser(t.Context(), userHandle(uid))
	require.NoError(t, err)
	sess, err := svc.FinishPasskeyLogin(t.Context(), breakglass.FinishLoginRequest{
		User: owner, Session: challenge.SessionData,
		Assertion: &protocol.ParsedCredentialAssertionData{Response: protocol.ParsedAssertionResponse{UserHandle: userHandle(uid)}},
	})
	require.ErrorIs(t, err, protocol.ErrVerification)
	assert.Nil(t, sess)
	var recorded int
	require.NoError(t, db.GetContext(t.Context(), &recorded, `SELECT COUNT(*) FROM breakglass_consumed_challenges`))
	assert.Zero(t, recorded, "a rejected assertion spends no challenge")
}
