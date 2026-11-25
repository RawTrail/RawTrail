package breakglass_test

import (
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/identity/internal/breakglass"
)

// spec:server-identity-authentication/a-laptop-s-owner-signs-in-with-their-passkey-alone/a-challenge-expires-after-five-minutes
//
// The challenge rides a signed cookie with no server-side row, so the server itself must expire it: the engine stamps an expiry at begin,
// and refuses to validate against a challenge past it, whatever the assertion.
func TestNewWebAuthn_EnforcesTheChallengeLifetime(t *testing.T) {
	t.Parallel()
	engine, err := breakglass.NewWebAuthn(breakglass.WebAuthnOptions{
		RPID: "localhost", RPDisplayName: "RawTrail test", RPOrigins: []string{"https://localhost:8443"},
	})
	require.NoError(t, err)
	user := breakglass.User{ID: 7, Email: "owner@example.com", Credentials: []webauthn.Credential{{ID: []byte("cred-1")}}}

	before := time.Now()
	_, session, err := engine.BeginLogin(user, webauthn.WithUserVerification(protocol.VerificationRequired))
	require.NoError(t, err)
	assert.WithinDuration(t, before.Add(5*time.Minute), session.Expires, 5*time.Second, "a login challenge expires in five minutes")

	_, registration, err := engine.BeginRegistration(user)
	require.NoError(t, err)
	assert.WithinDuration(t, before.Add(5*time.Minute), registration.Expires, 5*time.Second, "so does a registration challenge")

	session.Expires = time.Now().Add(-time.Second)
	_, err = engine.ValidateLogin(user, *session, &protocol.ParsedCredentialAssertionData{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Session has Expired")
}
