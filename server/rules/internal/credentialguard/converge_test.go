package credentialguard

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/rules/api"
)

// A command carries the policy only when it names its version and its update time: the same version with another epoch is another
// policy, as after a database restore sent the version backwards.
func TestCarries(t *testing.T) {
	t.Parallel()
	updated := time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC)
	policy := api.CredentialGuardPolicy{Version: 3, Mode: api.CredentialGuardProtect, UpdatedAt: &updated}
	micros := strconv.FormatInt(updated.UnixMicro(), 10)
	cases := []struct {
		name    string
		payload string
		want    bool
	}{
		{"the current policy", `{"version":3,"epoch":` + micros + `,"mode":"protect","credentials":[]}`, true},
		{"an older version", `{"version":2,"epoch":` + micros + `,"mode":"protect","credentials":[]}`, false},
		{"the same version from before a restore", `{"version":3,"epoch":1,"mode":"protect","credentials":[]}`, false},
		{"a payload it cannot read", `{`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, carries([]byte(tc.payload), policy))
		})
	}
	t.Run("the default policy, which has no update time", func(t *testing.T) {
		t.Parallel()
		assert.True(t, carries(commandPayload(api.DefaultCredentialGuard()), api.DefaultCredentialGuard()))
	})
	// spec:server-admin-surface/hosts-that-miss-the-credential-guard-push-get-it/a-host-holding-an-older-default-list-is-sent-the-new-one
	t.Run("the first default list, from before the AWS caches", func(t *testing.T) {
		t.Parallel()
		assert.False(t, carries([]byte(`{"version":0,"epoch":0,"mode":"detect","credentials":[]}`), api.DefaultCredentialGuard()),
			"a host holding the first default list is sent the revised one")
		var pushed api.SetCredentialGuardPayload
		require.NoError(t, json.Unmarshal(commandPayload(api.DefaultCredentialGuard()), &pushed))
		assert.Equal(t, int64(1), pushed.Epoch, "the default is pushed with its list's revision")
	})
}
