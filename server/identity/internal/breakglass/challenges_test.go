//go:build integration

package breakglass_test

import (
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/identity/internal/breakglass"
)

// zeroCounter makes the registered credential a zero-counter passkey, as Apple's are: stored at 0 and asserting 0 every time, so the
// sign-count regression check never fires.
func zeroCounter(t *testing.T, db *sqlx.DB, fake *fakeWebAuthnEngine) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `UPDATE webauthn_credentials SET sign_count = 0`)
	require.NoError(t, err)
	fake.loginCredential.Authenticator.SignCount = 0
}

// spec:server-identity-authentication/break-glass-webauthn-challenges-are-single-use/a-repeated-challenge-is-refused
//
// A zero-counter passkey's successful passkey-only sign-in cannot be submitted again: the same challenge and assertion mint one session.
func TestService_FinishPasskeyLogin_RefusesARepeatedChallenge(t *testing.T) {
	t.Parallel()
	svc, db, _, uid, fake := newFakeService(t)
	user := registerFakeCredential(t, svc, db, uid, fake)
	zeroCounter(t, db, fake)
	req := breakglass.FinishLoginRequest{
		User: user,
		Session: webauthn.SessionData{
			Challenge: "replayed-challenge", UserVerification: protocol.VerificationRequired, Expires: time.Now().Add(time.Minute),
		},
		Assertion: fakeAssertion(),
	}
	first, err := svc.FinishPasskeyLogin(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, first)

	again, err := svc.FinishPasskeyLogin(t.Context(), req)
	require.ErrorIs(t, err, breakglass.ErrChallengeReused)
	assert.Nil(t, again, "the repeat mints no session")

	other := req
	other.Session.Challenge = "a-fresh-challenge"
	_, err = svc.FinishPasskeyLogin(t.Context(), other)
	require.NoError(t, err, "a fresh challenge still signs in")
}

// spec:server-identity-authentication/break-glass-webauthn-challenges-are-single-use/a-repeated-challenge-is-refused
//
// The password sign-in, and the reauth that shares VerifyLogin, spend their challenge too, even when the password then fails.
func TestService_VerifyLogin_RefusesARepeatedChallenge(t *testing.T) {
	t.Parallel()
	svc, db, _, uid, fake := newFakeService(t)
	user := registerFakeCredential(t, svc, db, uid, fake)
	zeroCounter(t, db, fake)
	session := webauthn.SessionData{Challenge: "password-challenge", Expires: time.Now().Add(time.Minute)}

	err := svc.VerifyLogin(t.Context(), breakglass.FinishLoginRequest{
		User: user, Session: session, Password: "the-wrong-password", Assertion: fakeAssertion(),
	})
	require.Error(t, err)
	require.NotErrorIs(t, err, breakglass.ErrChallengeReused, "the first attempt fails on its password")

	err = svc.VerifyLogin(t.Context(), breakglass.FinishLoginRequest{
		User: user, Session: session, Password: "long-enough-password", Assertion: fakeAssertion(),
	})
	require.ErrorIs(t, err, breakglass.ErrChallengeReused, "the challenge was spent by the first attempt")
}

// spec:server-identity-authentication/break-glass-webauthn-challenges-are-single-use/expired-challenge-records-are-swept
func TestService_CleanupConsumedChallenges_RemovesOnlyExpiredRows(t *testing.T) {
	t.Parallel()
	svc, db, _, uid, fake := newFakeService(t)
	user := registerFakeCredential(t, svc, db, uid, fake)
	zeroCounter(t, db, fake)
	for challenge, expires := range map[string]time.Time{
		"expired": time.Now().Add(-time.Minute), "live": time.Now().Add(time.Minute),
	} {
		// FinishPasskeyLogin consumes before validating expiry only through the fake engine, which ignores Expires; the real engine
		// refuses an expired challenge before it is consumed. Here it seeds one row of each.
		_, err := svc.FinishPasskeyLogin(t.Context(), breakglass.FinishLoginRequest{
			User:      user,
			Session:   webauthn.SessionData{Challenge: challenge, UserVerification: protocol.VerificationRequired, Expires: expires},
			Assertion: fakeAssertion(),
		})
		require.NoError(t, err, challenge)
	}
	removed, err := svc.CleanupConsumedChallenges(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed)
	var left int
	require.NoError(t, db.GetContext(t.Context(), &left,
		`SELECT COUNT(*) FROM breakglass_consumed_challenges WHERE challenge_hash = UNHEX(SHA2('live', 256))`))
	assert.Equal(t, 1, left, "the live challenge's record is the one that stays")
}
