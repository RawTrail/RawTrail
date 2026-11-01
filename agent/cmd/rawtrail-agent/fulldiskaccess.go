package main

import (
	"context"
	"time"

	"github.com/rawtrail/rawtrail/agent/fda"
	"github.com/rawtrail/rawtrail/agent/health"
)

// applyFullDiskAccess reports what a probe finds about Full Disk Access (issue #1436): the agent's own grant as its component, and a
// refusal the extension recorded as the reason the security extension is not running. A read that says nothing (the privacy database
// missing, no refusal recorded) changes nothing.
func applyFullDiskAccess(reg *health.Registry, probe fda.Probe) {
	switch probe.AgentGrant() {
	case fda.GrantGiven:
		reg.MarkFullDiskAccessGranted(health.ComponentAgentFullDiskAccess)
	case fda.GrantMissing:
		reg.MarkFullDiskAccessMissing(health.ComponentAgentFullDiskAccess)
	case fda.GrantUnknown:
	}
	if probe.ExtensionRefusal() == fda.RefusedNotPermitted {
		reg.MarkFullDiskAccessMissing(health.ComponentEndpointSecurityExtension)
	}
}

// watchFullDiskAccessWith registers the agent's own Full Disk Access component and keeps it, and the security extension's refusal,
// current every interval until ctx ends.
func watchFullDiskAccessWith(ctx context.Context, reg *health.Registry, probe fda.Probe, interval time.Duration) {
	reg.Register(health.ComponentAgentFullDiskAccess, "Agent")
	applyFullDiskAccess(reg, probe)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			applyFullDiskAccess(reg, probe)
		}
	}
}
