//go:build darwin

package main

import (
	"context"
	"time"

	"github.com/rawtrail/rawtrail/agent/fda"
	"github.com/rawtrail/rawtrail/agent/health"
)

// fullDiskAccessInterval is how often the agent rechecks Full Disk Access: often enough that a grant turned on in System Settings shows
// within a check-in or two, rare enough to cost nothing.
const fullDiskAccessInterval = 30 * time.Second

// watchFullDiskAccess registers the agent's own Full Disk Access component and keeps it, and the security extension's refusal, current
// until ctx ends (issue #1436).
func watchFullDiskAccess(ctx context.Context, reg *health.Registry) {
	reg.Register(health.ComponentAgentFullDiskAccess, "Agent")
	probe := fda.NewProbe()
	applyFullDiskAccess(reg, probe)
	ticker := time.NewTicker(fullDiskAccessInterval)
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
