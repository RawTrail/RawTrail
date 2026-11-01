package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/agent/fda"
	"github.com/rawtrail/rawtrail/agent/health"
)

func reasons(reg *health.Registry) map[string]string {
	out := map[string]string{}
	for _, c := range reg.Snapshot() {
		out[c.Type] = c.Reason
	}
	return out
}

// spec:agent-status-reporting/a-missing-full-disk-access-grant-is-reported/a-refused-extension-reports-the-missing-grant
func TestApplyFullDiskAccess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tcc := filepath.Join(dir, "TCC.db")
	require.NoError(t, os.WriteFile(tcc, []byte("db"), 0o600))
	refusal := filepath.Join(dir, "es-client-status.json")

	reg := health.NewRegistry()
	reg.Register(health.ComponentEndpointSecurityExtension, "Security extension")

	// A privacy database that cannot be found says nothing, so the agent reports no component for its grant.
	applyFullDiskAccess(reg, fda.Probe{TCCPath: filepath.Join(dir, "absent.db"), StatusPath: refusal})
	assert.Equal(t, map[string]string{health.ComponentEndpointSecurityExtension: "never_connected"}, reasons(reg))

	probe := fda.Probe{TCCPath: tcc, StatusPath: refusal}

	// No refusal recorded: the extension keeps never_connected, and the agent's readable database is a grant.
	applyFullDiskAccess(reg, probe)
	assert.Equal(t, map[string]string{health.ComponentEndpointSecurityExtension: "never_connected",
		health.ComponentAgentFullDiskAccess: "full_disk_access_granted"}, reasons(reg))

	// Another refusal is not a missing grant.
	require.NoError(t, os.WriteFile(refusal, []byte(`{"refused":"too_many_clients","at":"x"}`), 0o600))
	applyFullDiskAccess(reg, probe)
	assert.Equal(t, "never_connected", reasons(reg)[health.ComponentEndpointSecurityExtension])

	require.NoError(t, os.WriteFile(refusal, []byte(`{"refused":"not_permitted","at":"x"}`), 0o600))
	applyFullDiskAccess(reg, probe)
	assert.Equal(t, health.ReasonFullDiskAccessMissing, reasons(reg)[health.ComponentEndpointSecurityExtension])

	// Nor does it, once the agent has read its grant: the last reading stands.
	applyFullDiskAccess(reg, fda.Probe{TCCPath: filepath.Join(dir, "absent.db"), StatusPath: refusal})
	assert.Equal(t, "full_disk_access_granted", reasons(reg)[health.ComponentAgentFullDiskAccess])

	// One it may not read is the grant revoked. (A file mode refuses everyone but root, and nobody on Windows, so the case needs a
	// non-root test run off Windows.)
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		return
	}
	refused := filepath.Join(dir, "refused.db")
	require.NoError(t, os.WriteFile(refused, []byte("db"), 0o000))
	applyFullDiskAccess(reg, fda.Probe{TCCPath: refused, StatusPath: refusal})
	assert.Equal(t, health.ReasonFullDiskAccessMissing, reasons(reg)[health.ComponentAgentFullDiskAccess])
}

func TestWatchFullDiskAccessWith(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	refusal := filepath.Join(dir, "es-client-status.json")
	reg := health.NewRegistry()
	reg.Register(health.ComponentEndpointSecurityExtension, "Security extension")

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		watchFullDiskAccessWith(ctx, reg, fda.Probe{TCCPath: filepath.Join(dir, "TCC.db"), StatusPath: refusal}, time.Millisecond)
		close(done)
	}()

	// It picks up a refusal recorded after it started.
	require.NoError(t, os.WriteFile(refusal, []byte(`{"refused":"not_permitted","at":"x"}`), 0o600))
	require.Eventually(t, func() bool {
		return reasons(reg)[health.ComponentEndpointSecurityExtension] == health.ReasonFullDiskAccessMissing
	}, time.Second, time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the watch did not stop when its context ended")
	}
}
