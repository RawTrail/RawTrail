package commander

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runCredentialGuard executes one set_credential_guard command and returns the terminal status and decoded result.
func runCredentialGuard(t *testing.T, sender ExtensionSender, payload string) (string, map[string]any) {
	t.Helper()
	var status string
	var result json.RawMessage
	report := func(_ context.Context, s string, r json.RawMessage) error {
		status, result = s, r
		return nil
	}
	cmd := Command{ID: 1, CommandType: "set_credential_guard", Payload: json.RawMessage(payload)}
	NewExecutor(sender, nil, nil).Execute(t.Context(), cmd, report)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(result, &decoded))
	return status, decoded
}

// spec:agent-command-executor/set-credential-guard-command/a-credential-guard-policy-is-forwarded
func TestExecuteSetCredentialGuard_ForwardsTheRawPayload(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		raw  string
		want map[string]any
	}{
		{"a changed policy", `{"version":4,"epoch":1789300000000000,"mode":"protect","credentials":[{"kind":"netrc","path":".netrc",` +
			`"match":"literal","owners":[{"signing_id":"com.apple.curl"}]}]}`,
			map[string]any{"version": float64(4), "mode": "protect", "credentials": float64(1)}},
		// Version 0 is the policy no operator has changed, which the server sends so a host learns the fleet's default.
		{"the policy no operator has changed", `{"version":0,"epoch":0,"mode":"detect","credentials":[]}`,
			map[string]any{"version": float64(0), "mode": "detect", "credentials": float64(0)}},
		// The mode is the extension's to judge: one this agent has not heard of still reaches it.
		{"a mode the agent does not know", `{"version":2,"epoch":5,"mode":"a-newer-mode","credentials":[]}`,
			map[string]any{"version": float64(2), "mode": "a-newer-mode", "credentials": float64(0)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sender := &recordingExtensionSender{}
			status, result := runCredentialGuard(t, sender, tc.raw)
			assert.Equal(t, StatusCompleted, status)
			// Byte equality: the extension decodes what the server wrote, not a re-marshalled copy.
			assert.Equal(t, [][]byte{[]byte(tc.raw)}, sender.guard)
			assert.Empty(t, sender.watched, "a policy is not a watched-path set")
			assert.Equal(t, tc.want, result)
		})
	}
}

// spec:agent-command-executor/set-credential-guard-command/a-credential-guard-payload-is-invalid
func TestExecuteSetCredentialGuard_RejectsAnInvalidEnvelope(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		payload string
		reason  string
	}{
		{"not JSON", `{`, "invalid payload"},
		{"no version", `{"mode":"detect","credentials":[]}`, "payload missing version"},
		{"a null version", `{"version":null,"mode":"detect","credentials":[]}`, "payload missing version"},
		{"a negative version", `{"version":-1,"mode":"detect","credentials":[]}`, "payload has a negative version"},
		{"no mode", `{"version":1,"credentials":[]}`, "payload missing mode"},
		{"no credentials", `{"version":1,"mode":"detect"}`, "payload missing or invalid credentials array"},
		{"credentials not an array", `{"version":1,"mode":"detect","credentials":{}}`, "payload missing or invalid credentials array"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sender := &recordingExtensionSender{}
			status, result := runCredentialGuard(t, sender, tc.payload)
			assert.Equal(t, StatusFailed, status)
			assert.Contains(t, result["error"], tc.reason)
			assert.Empty(t, sender.guard, "nothing invalid reaches the extension")
		})
	}
}

// A policy that cannot reach the extension fails the command with the reason, as a watched-path set does.
func TestExecuteSetCredentialGuard_ReportsASendFailure(t *testing.T) {
	t.Parallel()
	payload := `{"version":1,"mode":"detect","credentials":[]}`
	for _, tc := range []struct {
		name   string
		sender ExtensionSender
		reason string
	}{
		{"no sender", nil, "extension sender not configured"},
		{"the send fails", &recordingExtensionSender{sendErr: errors.New("no connector")}, "xpc send: no connector"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			status, result := runCredentialGuard(t, tc.sender, payload)
			assert.Equal(t, StatusFailed, status)
			assert.Equal(t, tc.reason, result["error"])
		})
	}
}
