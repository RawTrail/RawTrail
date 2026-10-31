//go:build !darwin

package main

import (
	"context"

	"github.com/rawtrail/rawtrail/agent/health"
)

// watchFullDiskAccess has nothing to watch off macOS, where no privacy grant stands between the agent and what it reads.
func watchFullDiskAccess(context.Context, *health.Registry) {}
