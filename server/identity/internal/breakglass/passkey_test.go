//go:build integration

package breakglass_test

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/identity/api"
	"github.com/rawtrail/rawtrail/server/identity/internal/breakglass"
	"github.com/rawtrail/rawtrail/server/identity/internal/users"
)

// registerFakeCredential redeems a setup token for the seeded admin, so a credential row exists, and primes the fake engine to assert
// with it at a sign count above the stored one. It returns the admin as stored after redemption.
func registerFakeCredential(t *testing.T, svc *breakglass.Service, db *sqlx.DB, uid int64, fake *fakeWebAuthnEngine) *users.User {
	t.Helper()
	plaintext, _, err := svc.IssueSetupToken(t.Context(), uid, time.Hour)
	require.NoError(t, err)
	_, tok, user, err := svc.BeginSetup(t.Context(), plaintext)
	require.NoError(t, err)
	_, err = svc.FinishSetup(t.Context(), breakglass.FinishSetupRequest{
		Token: tok, User: user, Session: webauthn.SessionData{Challenge: "fake-challenge"},
		Password: "long-enough-password", Attestation: fakeAttestation(),
	})
	require.NoError(t, err)
	rows, err := breakglass.NewCredentialStore(db).ListByUserID(t.Context(), uid)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	fake.loginCredential = &webauthn.Credential{
		ID: rows[0].CredentialID, PublicKey: []byte("fake-public-key"),
		Authenticator: webauthn.Authenticator{SignCount: uint32(rows[0].SignCount) + 1},
	}
	stored, err := users.New(db).GetByEmail(t.Context(), user.Email)
	require.NoError(t, err)
	return stored
}

// The passkey-only challenge requires the authenticator to verify the person; the password sign-in's does not.
func TestService_BeginPasskeyLogin_RequiresUserVerification(t *testing.T) {
	t.Parallel()
	svc, db, _, uid, fake := newFakeService(t)
	registerFakeCredential(t, svc, db, uid, fake)

	passkey, _, err := svc.BeginPasskeyLogin(t.Context(), "admin@rawtrail.local")
	require.NoError(t, err)
	assert.Equal(t, protocol.VerificationRequired, passkey.SessionData.UserVerification)
	assert.Equal(t, protocol.VerificationRequired, passkey.Options.Response.UserVerification, "the browser is told to verify the person")

	password, _, err := svc.BeginLogin(t.Context(), "admin@rawtrail.local")
	require.NoError(t, err)
	assert.NotEqual(t, protocol.VerificationRequired, password.SessionData.UserVerification)
}

// spec:server-identity-authentication/a-laptop-s-owner-signs-in-with-their-passkey-alone/a-verified-passkey-signs-the-owner-in
func TestService_FinishPasskeyLogin_MintsASessionWithoutAPassword(t *testing.T) {
	t.Parallel()
	svc, db, _, uid, fake := newFakeService(t)
	user := registerFakeCredential(t, svc, db, uid, fake)

	sess, err := svc.FinishPasskeyLogin(t.Context(), breakglass.FinishLoginRequest{
		User:      user,
		Session:   verifiedSession(uid),
		Assertion: fakeAssertion(),
	})
	require.NoError(t, err)
	assert.Equal(t, uid, sess.UserID)
	rows, err := breakglass.NewCredentialStore(db).ListByUserID(t.Context(), uid)
	require.NoError(t, err)
	assert.Equal(t, uint64(fake.loginCredential.Authenticator.SignCount), rows[0].SignCount, "the sign count is recorded")
}

// spec:server-identity-authentication/a-laptop-s-owner-signs-in-with-their-passkey-alone/an-unverified-passkey-is-refused
func TestService_FinishPasskeyLogin_RefusesAChallengeWithoutUserVerification(t *testing.T) {
	t.Parallel()
	for _, uv := range []protocol.UserVerificationRequirement{"", protocol.VerificationPreferred, protocol.VerificationDiscouraged} {
		t.Run(string(uv), func(t *testing.T) {
			t.Parallel()
			svc, db, _, uid, fake := newFakeService(t)
			user := registerFakeCredential(t, svc, db, uid, fake)
			sess, err := svc.FinishPasskeyLogin(t.Context(), breakglass.FinishLoginRequest{
				User: user, Session: webauthn.SessionData{Challenge: "fake-challenge", UserVerification: uv}, Assertion: fakeAssertion(),
			})
			require.ErrorIs(t, err, breakglass.ErrUserVerificationRequired)
			assert.Nil(t, sess)
			rows, err := breakglass.NewCredentialStore(db).ListByUserID(t.Context(), uid)
			require.NoError(t, err)
			assert.Equal(t, uint64(1), rows[0].SignCount, "a refused sign-in records nothing")
		})
	}
}

// A passkey sign-in still fails when the assertion does: no password is a fallback for a bad signature.
func TestService_FinishPasskeyLogin_RefusesABadAssertion(t *testing.T) {
	t.Parallel()
	svc, db, _, uid, fake := newFakeService(t)
	user := registerFakeCredential(t, svc, db, uid, fake)
	fake.validateLoginErr = protocol.ErrVerification
	_, err := svc.FinishPasskeyLogin(t.Context(), breakglass.FinishLoginRequest{
		User:      user,
		Session:   verifiedSession(uid),
		Assertion: fakeAssertion(),
	})
	require.ErrorIs(t, err, protocol.ErrVerification)
}

func newPasskeyServer(t *testing.T, passkeyOnly bool) (*httptest.Server, *breakglass.Service, *sqlx.DB, *recAudit, int64, *fakeWebAuthnEngine) {
	t.Helper()
	svc, db, rec, uid, fake := newFakeService(t)
	h := breakglass.NewHandler(breakglass.HandlerOptions{
		Service: svc, SigningKey: []byte("0123456789abcdef0123456789abcdef"), Logger: slog.Default(), PasskeyOnly: passkeyOnly,
	})
	mux := http.NewServeMux()
	h.RegisterPublicRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, svc, db, rec, uid, fake
}

func postJSON(t *testing.T, srv *httptest.Server, path, body string, cookie *http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func cookieNamed(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// parseableAssertion is an assertion body protocol.ParseCredentialRequestResponseBytes accepts; the fake engine decides its validity.
func parseableAssertion(t *testing.T) string {
	t.Helper()
	return assertionBody(t, map[string]any{"email": "admin@rawtrail.local"}, nil)
}

// assertionBody is a sign-in body of fields plus a parseable assertion, which carries userHandle when it is set.
func assertionBody(t *testing.T, fields map[string]any, userHandle []byte) string {
	t.Helper()
	clientData, err := json.Marshal(map[string]any{"type": "webauthn.get", "challenge": "fake-challenge", "origin": "http://localhost:8088"})
	require.NoError(t, err)
	response := map[string]any{
		"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientData),
		"authenticatorData": base64.RawURLEncoding.EncodeToString(make([]byte, 37)),
		"signature":         base64.RawURLEncoding.EncodeToString(make([]byte, 8)),
	}
	if userHandle != nil {
		response["userHandle"] = base64.RawURLEncoding.EncodeToString(userHandle)
	}
	body := maps.Clone(fields)
	body["assertion"] = map[string]any{
		"id": base64.RawURLEncoding.EncodeToString([]byte("cred-id-1")), "rawId": base64.RawURLEncoding.EncodeToString([]byte("cred-id-1")),
		"type": "public-key", "response": response,
	}
	out, err := json.Marshal(body)
	require.NoError(t, err)
	return string(out)
}

// userHandle is the WebAuthn user handle of the account with id uid.
func userHandle(uid int64) []byte { return breakglass.User{ID: uid}.WebAuthnID() }

// verifiedSession is the session data of a passkey-only challenge naming the account uid, as BeginPasskeyLogin issues it.
func verifiedSession(uid int64) webauthn.SessionData {
	return webauthn.SessionData{
		Challenge: "fake-challenge", UserID: userHandle(uid), UserVerification: protocol.VerificationRequired, Expires: time.Now().Add(time.Minute),
	}
}

// spec:server-identity-authentication/a-laptop-s-owner-signs-in-with-their-passkey-alone/a-verified-passkey-signs-the-owner-in
//
// Through the laptop's routes: the challenge requires user verification, and the finish, with no password, sets the session cookie and
// audits the sign-in as passkey-only.
func TestHandle_PasskeyLogin_SignsTheLaptopOwnerIn(t *testing.T) {
	t.Parallel()
	srv, svc, db, rec, uid, fake := newPasskeyServer(t, true)
	registerFakeCredential(t, svc, db, uid, fake)

	challenge := postJSON(t, srv, "/admin/break-glass/passkey/challenge", `{"email":"admin@rawtrail.local"}`, nil)
	require.Equal(t, http.StatusOK, challenge.StatusCode)
	state := cookieNamed(challenge, breakglass.ChallengeStateCookieName)
	require.NotNil(t, state)
	sd, err := breakglass.DecodeChallengeState([]byte("0123456789abcdef0123456789abcdef"), state.Value)
	require.NoError(t, err)
	assert.Equal(t, protocol.VerificationRequired, sd.UserVerification)

	finish := postJSON(t, srv, "/admin/break-glass/passkey", parseableAssertion(t), state)
	require.Equal(t, http.StatusOK, finish.StatusCode, "reason=%s", finish.Header.Get("X-Edr-Auth-Reason"))
	require.NotNil(t, cookieNamed(finish, api.SessionCookieName))
	last := rec.events[len(rec.events)-1]
	assert.Equal(t, api.AuditAuthBreakglassSuccess, last.Action)
	assert.Equal(t, "passkey_only", last.Payload["method"])
}

// spec:server-identity-authentication/a-laptop-s-owner-signs-in-with-their-passkey-alone/an-unverified-passkey-is-refused
//
// The password sign-in's challenge, which does not require user verification, cannot finish a passkey-only sign-in.
func TestHandle_PasskeyLogin_RefusesThePasswordSignInsChallenge(t *testing.T) {
	t.Parallel()
	srv, svc, db, rec, uid, fake := newPasskeyServer(t, true)
	registerFakeCredential(t, svc, db, uid, fake)

	challenge := postJSON(t, srv, "/admin/break-glass/challenge", `{"email":"admin@rawtrail.local"}`, nil)
	require.Equal(t, http.StatusOK, challenge.StatusCode)
	finish := postJSON(t, srv, "/admin/break-glass/passkey", parseableAssertion(t),
		cookieNamed(challenge, breakglass.ChallengeStateCookieName))
	assert.Equal(t, http.StatusUnauthorized, finish.StatusCode)
	assert.Nil(t, cookieNamed(finish, api.SessionCookieName))
	last := rec.events[len(rec.events)-1]
	assert.Equal(t, api.AuditAuthBreakglassFailure, last.Action)
	assert.Equal(t, "webauthn.user_verification_required", last.Payload["reason"])
}

// spec:server-identity-authentication/a-laptop-s-owner-signs-in-with-their-passkey-alone/a-server-install-offers-no-passkey-only-sign-in
func TestHandle_PasskeyLogin_IsAbsentOnAServer(t *testing.T) {
	t.Parallel()
	srv, _, _, _, _, _ := newPasskeyServer(t, false)
	for _, path := range []string{"/admin/break-glass/passkey/challenge", "/admin/break-glass/passkey"} {
		assert.Equal(t, http.StatusNotFound, postJSON(t, srv, path, `{"email":"admin@rawtrail.local"}`, nil).StatusCode, path)
	}
}

// Every early refusal on the sign-in routes writes auth.breakglass.failure with the refusal's reason, before the service is reached:
// the routes share the password sign-in's code, so this holds for it too.
func TestHandle_PasskeyLogin_AuditsEveryEarlyRefusal(t *testing.T) {
	t.Parallel()
	srv, svc, db, rec, uid, fake := newPasskeyServer(t, true)
	registerFakeCredential(t, svc, db, uid, fake)
	challenge := postJSON(t, srv, "/admin/break-glass/passkey/challenge", `{"email":"admin@rawtrail.local"}`, nil)
	require.Equal(t, http.StatusOK, challenge.StatusCode)
	state := cookieNamed(challenge, breakglass.ChallengeStateCookieName)

	cases := []struct {
		name, path, body string
		cookie           *http.Cookie
		want             string
	}{
		{"no challenge cookie", "/admin/break-glass/passkey", parseableAssertion(t), nil, "challenge_missing"},
		{"a forged challenge cookie", "/admin/break-glass/passkey", parseableAssertion(t),
			&http.Cookie{Name: breakglass.ChallengeStateCookieName, Value: "forged.value"}, "challenge_invalid"},
		{"a body that is not JSON", "/admin/break-glass/passkey", `{`, state, "body_invalid"},
		{"an assertion that does not parse", "/admin/break-glass/passkey", `{"email":"admin@rawtrail.local","assertion":{}}`, state,
			"assertion_parse_failed"},
		{"an unknown email at the challenge", "/admin/break-glass/passkey/challenge", `{"email":"nobody@example.com"}`, nil,
			"no_credentials"},
	}
	for _, tc := range cases {
		before := len(rec.events)
		resp := postJSON(t, srv, tc.path, tc.body, tc.cookie)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, tc.name)
		require.Len(t, rec.events, before+1, tc.name)
		last := rec.events[len(rec.events)-1]
		assert.Equal(t, api.AuditAuthBreakglassFailure, last.Action, tc.name)
		assert.Equal(t, tc.want, last.Payload["reason"], tc.name)
	}
}

// spec:server-identity-authentication/break-glass-webauthn-challenges-are-single-use/a-repeated-challenge-is-refused
//
// Through the routes: the same challenge cookie and assertion, sent twice, sign in once. The repeat is a 401 audited as a reused
// challenge.
func TestHandle_PasskeyLogin_RefusesAReplay(t *testing.T) {
	t.Parallel()
	srv, svc, db, rec, uid, fake := newPasskeyServer(t, true)
	registerFakeCredential(t, svc, db, uid, fake)
	zeroCounter(t, db, fake)
	challenge := postJSON(t, srv, "/admin/break-glass/passkey/challenge", `{"email":"admin@rawtrail.local"}`, nil)
	state := cookieNamed(challenge, breakglass.ChallengeStateCookieName)
	body := parseableAssertion(t)

	first := postJSON(t, srv, "/admin/break-glass/passkey", body, state)
	require.Equal(t, http.StatusOK, first.StatusCode, "reason=%s", first.Header.Get("X-Edr-Auth-Reason"))
	replay := postJSON(t, srv, "/admin/break-glass/passkey", body, state)
	assert.Equal(t, http.StatusUnauthorized, replay.StatusCode)
	assert.Nil(t, cookieNamed(replay, api.SessionCookieName))
	last := rec.events[len(rec.events)-1]
	assert.Equal(t, api.AuditAuthBreakglassFailure, last.Action)
	assert.Equal(t, "webauthn.challenge_reused", last.Payload["reason"])
}
