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
			Challenge: "replayed-challenge", UserID: userHandle(uid), UserVerification: protocol.VerificationRequired,
			Expires: time.Now().Add(time.Minute),
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
	svc, db, _, _, _ := newFakeService(t)
	// A session zone ten hours ahead of UTC must not move the sweep: one connection, so the zone holds for the service's statements.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	_, err := db.ExecContext(t.Context(), `SET time_zone = '+10:00'`)
	require.NoError(t, err)
	// One record expired two hours ago, one a minute ago (inside the hour's grace), and one still live.
	_, err = db.ExecContext(t.Context(), `INSERT INTO breakglass_consumed_challenges (challenge_hash, expires_at) VALUES
		(UNHEX(SHA2('long-expired', 256)), DATE_SUB(UTC_TIMESTAMP(6), INTERVAL 2 HOUR)),
		(UNHEX(SHA2('just-expired', 256)), DATE_SUB(UTC_TIMESTAMP(6), INTERVAL 1 MINUTE)),
		(UNHEX(SHA2('live', 256)), DATE_ADD(UTC_TIMESTAMP(6), INTERVAL 1 MINUTE))`)
	require.NoError(t, err)
	removed, err := svc.CleanupConsumedChallenges(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed)
	var left []string
	require.NoError(t, db.SelectContext(t.Context(), &left, `SELECT HEX(challenge_hash) FROM breakglass_consumed_challenges
		WHERE challenge_hash IN (UNHEX(SHA2('just-expired', 256)), UNHEX(SHA2('live', 256)))`))
	assert.Len(t, left, 2, "a record inside the grace and a live one stay; only the long-expired one goes")
}

// spec:server-identity-authentication/break-glass-webauthn-challenges-are-single-use/an-expired-or-unexpiring-challenge-is-refused
//
// A challenge with no expiry, as a cookie from before the server enforced timeouts carries, or one already past it, is refused and not
// recorded. The second is the race a sweep opens: a replay that validated just before expiry and reaches the insert after the sweep
// removed the original record is refused, not recorded afresh.
func TestService_FinishPasskeyLogin_RefusesAnExpiredOrUnexpiringChallenge(t *testing.T) {
	t.Parallel()
	for name, expires := range map[string]time.Time{"no expiry": {}, "already expired": time.Now().Add(-time.Second)} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc, db, _, uid, fake := newFakeService(t)
			user := registerFakeCredential(t, svc, db, uid, fake)
			zeroCounter(t, db, fake)
			sess, err := svc.FinishPasskeyLogin(t.Context(), breakglass.FinishLoginRequest{
				User: user,
				Session: webauthn.SessionData{
					Challenge: "late", UserID: userHandle(uid), UserVerification: protocol.VerificationRequired, Expires: expires,
				},
				Assertion: fakeAssertion(),
			})
			require.ErrorIs(t, err, breakglass.ErrChallengeExpired)
			assert.Nil(t, sess)
			var recorded int
			require.NoError(t, db.GetContext(t.Context(), &recorded, `SELECT COUNT(*) FROM breakglass_consumed_challenges`))
			assert.Zero(t, recorded, "a refused challenge is not recorded")
		})
	}
}

// spec:server-identity-authentication/break-glass-webauthn-challenges-are-single-use/an-expired-or-unexpiring-challenge-is-refused
//
// Expiry is judged in UTC whatever the MySQL session's time zone: ten hours ahead of UTC a live challenge is still accepted, and ten
// hours behind an expired one is still refused. The pool is pinned to one connection so the session zone set here is the one the
// service's statements run under.
func TestService_FinishPasskeyLogin_JudgesExpiryInUTCWhateverTheSessionZone(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		zone    string
		expires time.Duration
		wantErr error
	}{
		{"+10:00", time.Minute, nil},
		{"-10:00", -time.Second, breakglass.ErrChallengeExpired},
	} {
		t.Run(tc.zone, func(t *testing.T) {
			t.Parallel()
			svc, db, _, uid, fake := newFakeService(t)
			user := registerFakeCredential(t, svc, db, uid, fake)
			zeroCounter(t, db, fake)
			db.SetMaxOpenConns(1)
			db.SetMaxIdleConns(1)
			_, err := db.ExecContext(t.Context(), `SET time_zone = ?`, tc.zone)
			require.NoError(t, err)
			_, err = svc.FinishPasskeyLogin(t.Context(), breakglass.FinishLoginRequest{
				User: user,
				Session: webauthn.SessionData{
					Challenge: "zoned", UserID: userHandle(uid), UserVerification: protocol.VerificationRequired,
					Expires: time.Now().Add(tc.expires),
				},
				Assertion: fakeAssertion(),
			})
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}
