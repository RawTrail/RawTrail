package credentialguard

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

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
}
