package watchedpaths

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/rawtrail/rawtrail/server/catchup"
	"github.com/rawtrail/rawtrail/server/rules/api"
	"github.com/rawtrail/rawtrail/server/rules/internal/hostpush"
)

// DefaultConvergeInterval is how often the catch-up runs. A host that enrolls, reinstalls, or comes back after its command aged out
// gets the set within this long of its next poll. The policy, including how long a failed command counts as delivered, is shared with
// the containment catch-up (issue #1071).
const DefaultConvergeInterval = catchup.DefaultInterval

// Converger queues the current set for hosts that do not have it (issue #998). The push on a change reaches the hosts enrolled then,
// but a queued command lives an hour, a host enrolled later has none, and a reinstall loses the extension's persisted set; this closes
// all three without the host asking.
type Converger struct {
	store  *Store
	sweep  hostpush.Catchup
	logger *slog.Logger
	// ensureDefaults, when set, puts the built-in paths in the stored set before each pass (Service.EnsureDefaults), so a deployment
	// that never configured a set still pushes them, and one restored from a backup regains them. See SetEnsureDefaults.
	ensureDefaults func(context.Context) error
}

// SetEnsureDefaults makes each Converge pass first ensure the built-in watched paths are in the stored set.
func (c *Converger) SetEnsureDefaults(ensure func(context.Context) error) { c.ensureDefaults = ensure }

// NewConverger builds a Converger. Every dependency is required.
func NewConverger(store *Store, commands func(ctx context.Context, hostIDs []string, commandType string, payload []byte) (int, error),
	enrollments api.HostEnrollmentLister, latest api.HostCommandLister, logger *slog.Logger) *Converger {
	if store == nil || commands == nil || enrollments == nil || latest == nil {
		panic("watchedpaths.NewConverger: store, commands, enrollments and latest are required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Converger{store: store, logger: logger, sweep: hostpush.Catchup{
		Subject: "watchedpaths", CommandType: api.CommandTypeSetWatchedPaths, Commands: commands, Enrollments: enrollments,
		Latest: latest, Logger: logger, Now: time.Now,
	}}
}

// Loop runs Converge every interval until ctx is cancelled; a zero or negative interval means DefaultConvergeInterval.
//
// Not leader-gated. Replicas that sweep at the same moment can each queue the set for the same host, and the extension turns the
// second copy away because it is not newer than the first, so a race costs a duplicate command row rather than a wrong result; a
// leader lock would hold a pooled connection for the life of the process to prevent that (issue #722).
func (c *Converger) Loop(ctx context.Context, interval time.Duration) {
	catchup.Loop(ctx, "watchedpaths", c.Converge, interval, c.logger)
}

// Converge queues the current set for every enrolled host that needs it and returns how many hosts it queued it for. Nothing is queued
// while the set has never been changed, since every host already watches the empty set.
func (c *Converger) Converge(ctx context.Context) (int, error) {
	if c.ensureDefaults != nil {
		// Logged and not fatal: a pass that could not add the defaults still converges hosts onto the set as it stands, and the
		// next pass tries again.
		if err := c.ensureDefaults(ctx); err != nil {
			c.logger.WarnContext(ctx, "watchedpaths: could not add the built-in paths to the set", "err", err)
		}
	}
	set, err := c.store.Get(ctx)
	if err != nil || set.Version == 0 {
		return 0, err
	}
	return c.sweep.Converge(ctx, set.Version, commandPayload(set), func(payload []byte) bool { return carriesSet(payload, set) })
}

// carriesSet reports whether a command's payload delivers the current set. The same version with a different epoch is a different
// set, as after a database restore that sent versions backwards.
func carriesSet(payload []byte, set api.WatchedPathSet) bool {
	var queued api.SetWatchedPathsPayload
	return json.Unmarshal(payload, &queued) == nil &&
		queued.Version == set.Version && queued.Epoch == set.UpdatedAt.UnixMicro()
}
