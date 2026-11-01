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

// watchFullDiskAccess watches the real privacy database and refusal record until ctx ends (issue #1436).
func watchFullDiskAccess(ctx context.Context, reg *health.Registry) {
	watchFullDiskAccessWith(ctx, reg, fda.NewProbe(), fullDiskAccessInterval)
}
