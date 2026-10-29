package agentsettings

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"

	"github.com/rawtrail/rawtrail/server/rules/api"
)

// A command carries the settings only when it names their version and their update time: the same version with another epoch is other
// settings, as after a database restore sent the version backwards.
func TestCarries(t *testing.T) {
	t.Parallel()
	updated := time.Date(2026, 10, 29, 7, 0, 0, 0, time.UTC)
	settings := api.AgentSettings{Version: 3, UpdatedAt: &updated}
	cases := []struct {
		name    string
		payload string
		want    bool
	}{
		{"the current settings", `{"version":3,"epoch":` + epoch(updated) + `,"capture_prompts":false}`, true},
		{"an older version", `{"version":2,"epoch":` + epoch(updated) + `,"capture_prompts":false}`, false},
		{"the same version from before a restore", `{"version":3,"epoch":1,"capture_prompts":false}`, false},
		{"a payload it cannot read", `{`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, carries([]byte(tc.payload), settings))
		})
	}
}

func epoch(at time.Time) string {
	b, _ := json.Marshal(at.UnixMicro())
	return string(b)
}

// The payload the push and the catch-up send decodes to what was stored, and a host's copy of it is recognized as current.
func TestCommandPayload_RoundTrips(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		updated := time.UnixMicro(rapid.Int64Range(1, 1<<50).Draw(t, "epoch")).UTC()
		settings := api.AgentSettings{
			Version: rapid.Int64Range(1, 1<<40).Draw(t, "version"), CapturePrompts: rapid.Bool().Draw(t, "capture"), UpdatedAt: &updated,
		}
		var got api.SetAgentSettingsPayload
		payload := commandPayload(settings)
		if err := json.Unmarshal(payload, &got); err != nil {
			t.Fatal(err)
		}
		assert.Equal(t, api.SetAgentSettingsPayload{
			Version: settings.Version, Epoch: updated.UnixMicro(), CapturePrompts: settings.CapturePrompts,
		}, got)
		assert.True(t, carries(payload, settings))
	})
}
