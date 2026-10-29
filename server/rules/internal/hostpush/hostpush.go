// Package hostpush pushes a fleet-wide, versioned document to every enrolled host as one command type, and catches up the hosts a push
// missed. The watched-path set (issue #998) and the agent settings (issue #1426) are its documents: each owns its storage, its payload
// and its audit row, and shares how they reach hosts.
package hostpush

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/rawtrail/rawtrail/server/auditoutbox"
	"github.com/rawtrail/rawtrail/server/catchup"
	"github.com/rawtrail/rawtrail/server/rules/api"
	"github.com/rawtrail/rawtrail/server/rules/internal/appcontrol"
)

// Counts is how a push to hosts went.
type Counts struct {
	// FanoutHosts is how many enrolled hosts the document was queued for, and FanoutFailed how many of those it could not be queued for.
	FanoutHosts  int `json:"fanout_hosts"`
	FanoutFailed int `json:"fanout_failed"`
	// FanoutSkippedReason says why the document was queued for no host when that was a failure rather than an empty fleet:
	// appcontrol.FanoutSkipReasonHostLister when the enrolled hosts could not be listed. Empty otherwise.
	FanoutSkippedReason string `json:"fanout_skipped_reason,omitempty"`
}

// AuditPayload adds the counts to an audit row's payload.
func (c Counts) AuditPayload(payload map[string]any) {
	payload["fanout_hosts"] = c.FanoutHosts
	payload["fanout_failed"] = c.FanoutFailed
	if c.FanoutSkippedReason != "" {
		payload["fanout_skipped_reason"] = c.FanoutSkippedReason
	}
}

// Pusher queues one document's command for hosts.
type Pusher struct {
	// Subject names the document in logs, such as "watchedpaths".
	Subject     string
	CommandType string
	Commands    appcontrol.CommandBatchInserter
	Hosts       appcontrol.HostLister
	Logger      *slog.Logger
}

// Fanout queues payload for every enrolled host in one batched insert and reports how many hosts it tried, how many it missed, and why
// it reached none when that was a failure. version only labels the logs.
func (p Pusher) Fanout(ctx context.Context, version int64, payload []byte) Counts {
	var counts Counts
	hostIDs, err := p.Hosts(ctx)
	if err != nil {
		p.Logger.WarnContext(ctx, p.Subject+": host list failed; not pushed", "version", version, "err", err)
		counts.FanoutSkippedReason = appcontrol.FanoutSkipReasonHostLister
		return counts
	}
	if len(hostIDs) == 0 {
		return counts
	}
	hostIDs = slices.Clone(hostIDs)
	slices.Sort(hostIDs)
	inserted, err := p.Commands(ctx, hostIDs, p.CommandType, payload)
	counts.FanoutHosts, counts.FanoutFailed = len(hostIDs), len(hostIDs)-inserted
	if err != nil {
		p.Logger.WarnContext(ctx, p.Subject+": queueing failed for some hosts",
			"version", version, "attempted", len(hostIDs), "inserted", inserted, "err", err)
	}
	return counts
}

// Sealer adds a push's host counts to a change's held audit entry and releases it, reporting false when the hold had already passed.
type Sealer func(ctx context.Context, id int64, entry auditoutbox.Entry) (bool, error)

// CompleteAudit seals a change's held audit entry, built with the push's counts (or the error building it), and delivers it. The change
// and its entry are already committed, so every failure here is logged rather than returned: at worst the row arrives late, or without
// the counts.
func (p Pusher) CompleteAudit(
	ctx context.Context, version, auditID int64, entry auditoutbox.Entry, err error, seal Sealer, drain *auditoutbox.Drain,
) {
	sealed := false
	if err == nil {
		sealed, err = seal(ctx, auditID, entry)
	}
	switch {
	case err != nil:
		p.Logger.WarnContext(ctx, p.Subject+": could not add the push's host counts to the audit entry; it is delivered without "+
			"them once its hold passes", "version", version, "err", err)
	case !sealed:
		p.Logger.WarnContext(ctx, p.Subject+": the push outlasted its audit entry's hold; the row is delivered without host counts",
			"version", version)
	}
	drain.DeliverSoon(ctx)
}

// Catchup queues a document for the enrolled hosts that do not have it.
type Catchup struct {
	// Subject names the document in logs, such as "watchedpaths".
	Subject     string
	CommandType string
	Commands    func(ctx context.Context, hostIDs []string, commandType string, payload []byte) (int, error)
	Enrollments api.HostEnrollmentLister
	Latest      api.HostCommandLister
	Logger      *slog.Logger
	Now         func() time.Time
}

// Converge queues payload for every enrolled host whose latest command of the type does not carry it, by carries, and returns how many
// hosts it queued it for. version only labels the logs.
func (c Catchup) Converge(ctx context.Context, version int64, payload []byte, carries func(payload []byte) bool) (int, error) {
	enrolled, err := c.Enrollments(ctx)
	if err != nil {
		return 0, err
	}
	hostIDs := make([]string, len(enrolled))
	for i, e := range enrolled {
		hostIDs[i] = e.HostID
	}
	latest, err := c.Latest(ctx, c.CommandType, hostIDs)
	if err != nil {
		return 0, err
	}
	now := c.Now()
	var stale []string
	for _, e := range enrolled {
		if Needs(latest[e.HostID], e, carries, now) {
			stale = append(stale, e.HostID)
		}
	}
	if len(stale) == 0 {
		return 0, nil
	}
	slices.Sort(stale)
	inserted, err := c.Commands(ctx, stale, c.CommandType, payload)
	c.Logger.InfoContext(ctx, c.Subject+": queued for hosts that did not have it", "version", version, "hosts", len(stale),
		"queued", inserted)
	return inserted, err
}

// Needs reports whether a host should be sent the document, given its latest command of the type. The decision is catchup's; what
// belongs here is whether that command's payload carries the current document. The zero HostCommand is a host that has never been
// sent one, and carries nothing.
func Needs(cmd api.HostCommand, e api.HostEnrollment, carries func(payload []byte) bool, now time.Time) bool {
	// This context records a terminal time as a zero value where the shared decision takes a nil, so an unfinished command reads as
	// unfinished rather than as one that completed at the zero time.
	var completedAt *time.Time
	if !cmd.CompletedAt.IsZero() {
		completedAt = &cmd.CompletedAt
	}
	return catchup.Needed(catchup.Latest{
		Queued:      cmd.Payload != nil,
		Carries:     cmd.Payload != nil && carries(cmd.Payload),
		CreatedAt:   cmd.CreatedAt,
		Status:      cmd.Status,
		CompletedAt: completedAt,
	}, e.EnrolledAt, now)
}
