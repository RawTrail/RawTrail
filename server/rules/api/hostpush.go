package api

import (
	"context"
	"time"

	"github.com/rawtrail/rawtrail/server/catchup"
)

// HostEnrollment is a host with an active enrollment and when it last enrolled, as a catch-up of a pushed document reads it. A
// re-enrollment follows a reinstall, which removes what the host kept, so a document queued before it may no longer be on the host.
type HostEnrollment struct {
	HostID     string
	EnrolledAt time.Time
}

// HostEnrollmentLister returns the hosts with an active enrollment. cmd/main adapts the endpoint context's enrollment list.
type HostEnrollmentLister func(ctx context.Context) ([]HostEnrollment, error)

// HostCommand is what a catch-up needs to know about a host's latest command of the type that carries its document.
type HostCommand struct {
	Payload []byte
	// Status is already the shared catch-up vocabulary, mapped by the context that owns the command lifecycle rather than cast from
	// its spelling here (issue #1071). A cast would make a rename there an unrecognized status here, which the catch-up leaves
	// alone, silently stopping the sweep for every host holding one.
	Status    catchup.Status
	CreatedAt time.Time
	// CompletedAt is when the command reached a terminal status, and zero while it has not.
	CompletedAt time.Time
}

// HostCommandLister returns, per host, its most recently queued command of a type. cmd/main adapts the response context's
// LatestOfType.
type HostCommandLister func(ctx context.Context, commandType string, hostIDs []string) (map[string]HostCommand, error)
