package fda

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentGrant(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	readable := filepath.Join(dir, "TCC.db")
	require.NoError(t, os.WriteFile(readable, []byte("db"), 0o600))

	assert.Equal(t, GrantGiven, Probe{TCCPath: readable}.AgentGrant())
	assert.Equal(t, GrantUnknown, Probe{TCCPath: filepath.Join(dir, "absent.db")}.AgentGrant(), "a missing file says nothing about the grant")

	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a file mode cannot refuse this user a read")
	}
	refused := filepath.Join(dir, "refused.db")
	require.NoError(t, os.WriteFile(refused, []byte("db"), 0o000))
	assert.Equal(t, GrantMissing, Probe{TCCPath: refused}.AgentGrant(), "a refused read is a missing grant")
}

// spec:agent-status-reporting/a-missing-full-disk-access-grant-is-reported/the-extension-records-why-its-client-was-refused
func TestExtensionRefusal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cases := []struct {
		name, body, want string
	}{
		// The record as ESClientStatus.swift writes it (ESClientStatusTests pins the same bytes).
		{"Full Disk Access missing", `{"refused":"not_permitted","at":"2026-11-01T02:26:40Z"}`, RefusedNotPermitted},
		{"another refusal", `{"refused":"too_many_clients","at":"2026-11-01T02:26:40Z"}`, "too_many_clients"},
		{"an unreadable record", `not json`, ""},
	}
	for _, tc := range cases {
		path := filepath.Join(dir, tc.name+".json")
		require.NoError(t, os.WriteFile(path, []byte(tc.body), 0o600))
		assert.Equal(t, tc.want, Probe{StatusPath: path}.ExtensionRefusal(), tc.name)
	}
	assert.Empty(t, Probe{StatusPath: filepath.Join(dir, "none.json")}.ExtensionRefusal(), "no record, no refusal")
}
