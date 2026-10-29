package watchedpaths

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/rawtrail/rawtrail/server/auditoutbox"
	identityapi "github.com/rawtrail/rawtrail/server/identity/api"
	"github.com/rawtrail/rawtrail/server/rules/api"
	"github.com/rawtrail/rawtrail/server/rules/internal/appcontrol"
	"github.com/rawtrail/rawtrail/server/rules/internal/hostpush"
)

// ErrReasonRequired is returned for a change without a reason. Every change is audited with its reason, and a blank one would leave
// the audit row saying nothing about why the fleet started watching different files.
var ErrReasonRequired = errors.New("watchedpaths: reason is required")

// Service stores the watched-path set and pushes it to hosts.
type Service struct {
	store  *Store
	push   hostpush.Pusher
	drain  *auditoutbox.Drain
	logger *slog.Logger
}

// NewService builds a Service. store, commands and hosts are required. drain delivers the audit entries changes commit; it may be nil
// outside production, which leaves them in the outbox and logs each change whose audit row goes undelivered.
func NewService(store *Store, commands appcontrol.CommandBatchInserter, hosts appcontrol.HostLister, drain *auditoutbox.Drain,
	logger *slog.Logger) *Service {
	if store == nil || commands == nil || hosts == nil {
		panic("watchedpaths.NewService: store, commands and hosts are required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	push := hostpush.Pusher{
		Subject: "watchedpaths", CommandType: api.CommandTypeSetWatchedPaths, Commands: commands, Hosts: hosts, Logger: logger,
	}
	return &Service{store: store, push: push, drain: drain, logger: logger}
}

// Get returns the stored set.
func (s *Service) Get(ctx context.Context) (api.WatchedPathSet, error) {
	return s.store.Get(ctx)
}

// ReplaceResult is a replaced set and how its push to hosts went.
type ReplaceResult struct {
	Set api.WatchedPathSet `json:"set"`
	hostpush.Counts
}

// Replace validates paths, stores them as the new set, queues a set_watched_paths command for every enrolled host, and audits the
// change with its reason.
//
// The stored set is authoritative once written, so a push that does not reach every host does not fail the change: the result counts
// the hosts it missed, and so does the audit row. The audit entry commits with the set and is delivered after the push, with its counts.
//
// A non-nil expectedVersion is the version the caller's edit started from; the change is refused with ErrVersionConflict when the set
// has moved on, so a stale edit cannot silently remove paths someone else added.
func (s *Service) Replace(
	ctx context.Context, actor *identityapi.Actor, reason string, paths []api.WatchedPath, expectedVersion *int64,
) (ReplaceResult, error) {
	if strings.TrimSpace(reason) == "" {
		return ReplaceResult{}, ErrReasonRequired
	}
	if err := api.ValidateWatchedPaths(paths); err != nil {
		return ReplaceResult{}, err
	}
	previous, set, auditID, err := s.store.Replace(ctx, paths, actor.Principal.ID, expectedVersion,
		func(previous, next api.WatchedPathSet) (auditoutbox.Entry, error) {
			return auditEntry(ctx, actor, reason, previous, ReplaceResult{Set: next}, false)
		})
	if err != nil {
		return ReplaceResult{}, err
	}
	result := ReplaceResult{Set: set, Counts: s.push.Fanout(ctx, set.Version, commandPayload(set))}
	entry, err := auditEntry(ctx, actor, reason, previous, result, true)
	s.push.CompleteAudit(ctx, set.Version, auditID, entry, err, s.store.SealAudit, s.drain)
	return result, nil
}

// commandPayload is the set_watched_paths payload for a stored set. The push and the catch-up both build it here, so a host that is
// caught up gets exactly what the push sent. The set must have been changed at least once, so it carries its update time. Marshalling
// a struct of strings and integers cannot fail.
func commandPayload(set api.WatchedPathSet) []byte {
	payload, _ := json.Marshal(api.SetWatchedPathsPayload{
		Version: set.Version, Epoch: set.UpdatedAt.UnixMicro(), Paths: api.PushedWatchedPaths(set),
	})
	return payload
}

// defaultsReason is the audit reason for the system change that brings a set up to this build's default paths.
const defaultsReason = "built-in watched paths changed: this server pushes new defaults to every host"

// EnsureDefaults brings the stored set up to this build's api.DefaultWatchedPaths when it was stored with others, or with none
// (issue #1167), by storing the operator's same paths again as a change by the system. That is what carries new defaults to hosts:
// the extension applies only a set newer than the last one it accepted, so the version and epoch have to move, and the ordinary
// push, catch-up and audit then do the rest. A no-op once the set carries the current defaults, which is every call but the first
// after an upgrade. Two replicas racing converge: the loser's conditional write conflicts, and on re-reading it finds nothing to do.
func (s *Service) EnsureDefaults(ctx context.Context) error {
	for range 2 {
		set, err := s.store.Get(ctx)
		if err != nil {
			return err
		}
		if slices.Equal(set.Defaults, api.DefaultWatchedPaths) {
			return nil
		}
		version := set.Version
		system := &identityapi.Actor{Principal: identityapi.SystemPrincipal()}
		_, err = s.Replace(ctx, system, defaultsReason, set.Paths, &version)
		if !errors.Is(err, ErrVersionConflict) {
			return err
		}
	}
	return nil
}

// auditEntry encodes a replacement's audit row: the reason, the previous and new sets, and, when withCounts, how the push went. It
// carries the request's trace, so the row is attributed to the request that made the change rather than the one that delivers it.
func auditEntry(
	ctx context.Context, actor *identityapi.Actor, reason string, previous api.WatchedPathSet, result ReplaceResult, withCounts bool,
) (auditoutbox.Entry, error) {
	set := result.Set
	payload := map[string]any{
		"reason":           reason,
		"version":          set.Version,
		"paths":            set.Paths,
		"previous_version": previous.Version,
		"previous_paths":   previous.Paths,
	}
	if withCounts {
		result.AuditPayload(payload)
	}
	return auditoutbox.Encode(identityapi.AuditEvent{
		Actor:      actor.Principal,
		Action:     identityapi.AuditDetectionConfigWatchedPathsUpdate,
		TargetType: "watched_path_set",
		TargetID:   strconv.FormatInt(set.Version, 10),
		TraceID:    identityapi.TraceIDFromContext(ctx),
		Payload:    payload,
	})
}
