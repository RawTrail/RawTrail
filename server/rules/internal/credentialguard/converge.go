package credentialguard

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/rawtrail/rawtrail/server/catchup"
	"github.com/rawtrail/rawtrail/server/rules/api"
	"github.com/rawtrail/rawtrail/server/rules/internal/hostpush"
)

// Converger queues the current policy for hosts that do not have them: a host enrolled after a change, one that reinstalled, and
// one whose command aged out undelivered.
type Converger struct {
	store *Store
	sweep hostpush.Catchup
}

// NewConverger builds a Converger. Every dependency but logger is required.
func NewConverger(store *Store, commands func(ctx context.Context, hostIDs []string, commandType string, payload []byte) (int, error),
	enrollments api.HostEnrollmentLister, latest api.HostCommandLister, logger *slog.Logger) *Converger {
	if store == nil || commands == nil || enrollments == nil || latest == nil {
		panic("credentialguard.NewConverger: store, commands, enrollments and latest are required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Converger{store: store, sweep: hostpush.Catchup{
		Subject: subject, CommandType: api.CommandTypeSetCredentialGuard, Commands: commands, Enrollments: enrollments, Latest: latest,
		Logger: logger, Now: time.Now,
	}}
}

// Loop runs Converge every interval until ctx is cancelled; a zero or negative interval means catchup.DefaultInterval. Not
// leader-gated, for the watched-path catch-up's reason: a race between replicas queues a duplicate the agent turns away as not newer.
func (c *Converger) Loop(ctx context.Context, interval time.Duration) {
	catchup.Loop(ctx, subject, c.Converge, interval, c.sweep.Logger)
}

// Converge queues the current policy for every enrolled host that needs it and returns how many hosts it queued it for. The policy no
// operator has changed is sent too, so a host learns the fleet's mode and list rather than keeping whatever its extension last had.
func (c *Converger) Converge(ctx context.Context) (int, error) {
	policy, err := c.store.Get(ctx)
	if err != nil {
		return 0, err
	}
	return c.sweep.Converge(ctx, policy.Version, commandPayload(policy), func(payload []byte) bool {
		return carries(payload, policy)
	})
}

// carries reports whether a command's payload delivers the current policy. The same version with a different epoch is a different
// policy, as after a database restore that sent versions backwards.
func carries(payload []byte, policy api.CredentialGuardPolicy) bool {
	var queued api.SetCredentialGuardPayload
	return json.Unmarshal(payload, &queued) == nil &&
		queued.Version == policy.Version && queued.Epoch == epoch(policy)
}
