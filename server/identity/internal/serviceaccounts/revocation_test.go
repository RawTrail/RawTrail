package serviceaccounts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCredentialGeneration(t *testing.T) {
	t.Parallel()
	clientID, err := generateClientID()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(clientID, clientIDPrefix), "client id carries its prefix")

	secret, err := generateSecret()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(secret, secretPrefix), "secret carries its scanning prefix")

	// Distinct calls produce distinct values.
	clientID2, err := generateClientID()
	require.NoError(t, err)
	assert.NotEqual(t, clientID, clientID2)
}

func TestSecretMatches(t *testing.T) {
	t.Parallel()
	secret, err := generateSecret()
	require.NoError(t, err)
	stored := hashSecret(secret)
	assert.True(t, SecretMatches(stored, secret), "the issued secret matches its stored hash")
	assert.False(t, SecretMatches(stored, secret+"x"), "a different secret does not match")
	assert.False(t, SecretMatches(stored, ""), "empty does not match")
}
