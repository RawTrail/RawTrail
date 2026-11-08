// Package containedsilence raises an alert when a network-contained host stops reporting (issue #1228).
//
// A contained host's isolation is enforced on the host and kept in place by the agent, which repairs the network filter if it is
// switched off and reports every such attempt. A local administrator who stops the agent first takes all of that away at once:
// nothing repairs the filter, and nothing reports the tamper. What the server still sees is the host falling silent, and for a host
// an operator has isolated, silence is worth a responder's attention rather than a grey "offline" pill.
//
// The check runs on every replica without a leader lock. The alert's dedup key names the containment, so two replicas raising it at
// once produce one row, and a race costs a duplicate insert attempt rather than a duplicate alert.
package containedsilence

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

const (
	// RuleID names the alert. It is not a catalog rule: it is raised from the server's own view of a host, not from events.
	RuleID = "contained_host_silent"
	// DefaultAfter is how long a contained host may stay silent before it alerts. Twice the five minutes after which the console
	// calls a host offline, so a contained laptop whose lid was closed briefly does not page anyone.
	DefaultAfter = 10 * time.Minute
	// DefaultInterval is how often the check runs.
	DefaultInterval = time.Minute
)

// Inserter persists an alert, reporting whether it was newly created rather than matched to an existing one.
type Inserter func(ctx context.Context, a api.Alert) (bool, error)

// Store is what the check reads and writes about hosts: when the server last heard from each, and which containment each last
// alerted for.
type Store interface {
	HostsCheckIn(ctx context.Context, hostIDs []string) (map[string]api.HostCheckIn, error)
	MarkSilentAlerted(ctx context.Context, hostID string, epoch int64) error
}

// Check finds contained hosts that have stopped reporting and alerts on each, once per containment.
type Check struct {
	contained func(ctx context.Context) ([]api.ContainedHost, error)
	store     Store
	insert    Inserter
	after     time.Duration
	now       func() time.Time
	logger    *slog.Logger
}

// New builds a Check. A zero after means DefaultAfter, and a nil now means time.Now.
func New(contained func(ctx context.Context) ([]api.ContainedHost, error), store Store, insert Inserter, after time.Duration,
	now func() time.Time, logger *slog.Logger) *Check {
	if after <= 0 {
		after = DefaultAfter
	}
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Check{contained: contained, store: store, insert: insert, after: after, now: now, logger: logger}
}

// Run raises an alert for every contained host silent for longer than the check's threshold, and reports how many were new.
//
// Silence is counted from the later of the host's last check-in and the moment it was contained. A host contained while already
// quiet gets the full threshold from the containment, so containing an offline laptop does not alert the moment the button is pressed.
//
// The check-in is the server's own observation, never a time the agent stamped on an event: a host whose clock runs ahead could
// otherwise keep itself "seen" long after it stopped reporting. And a host that already alerted for its containment is skipped on
// the store's record of it, not on the alert row, which alert retention can delete while the host is still silent.
func (c *Check) Run(ctx context.Context) (int, error) {
	hosts, err := c.contained(ctx)
	if err != nil {
		return 0, fmt.Errorf("list contained hosts: %w", err)
	}
	if len(hosts) == 0 {
		return 0, nil
	}
	ids := make([]string, len(hosts))
	for i, h := range hosts {
		ids[i] = h.HostID
	}
	checkIns, err := c.store.HostsCheckIn(ctx, ids)
	if err != nil {
		return 0, fmt.Errorf("read contained hosts' last check-in: %w", err)
	}
	now := c.now()
	created := 0
	// Every host is attempted whatever happens to the others: they are independent, and a host that fails every minute must not
	// keep the hosts after it from ever being alerted on.
	var errs []error
	for _, h := range hosts {
		checkIn := checkIns[h.HostID]
		if checkIn.SilentAlertedEpoch == h.Epoch {
			continue
		}
		quietSince := max(checkIn.LastCheckInNs, h.ContainedAtNs)
		silent := now.Sub(time.Unix(0, quietSince))
		if silent <= c.after {
			continue
		}
		isNew, err := c.insert(ctx, alertFor(h, silent))
		if err != nil {
			errs = append(errs, fmt.Errorf("alert on silent contained host %s: %w", h.HostID, err))
			continue
		}
		// Recorded after the alert, so a failure between the two re-runs the insert, which the alert's dedup key turns into a match.
		if err := c.store.MarkSilentAlerted(ctx, h.HostID, h.Epoch); err != nil {
			errs = append(errs, err)
		}
		if isNew {
			created++
			c.logger.WarnContext(ctx, "contained host stopped reporting", "host_id", h.HostID,
				"silent_for", silent.Round(time.Second), "containment_epoch", h.Epoch)
		}
	}
	return created, errors.Join(errs...)
}

// Loop runs the check at once and then every interval until ctx is cancelled; a zero interval means DefaultInterval. The first run
// is immediate so a server that restarts does not leave a silent contained host unreported for another interval.
func (c *Check) Loop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := c.Run(ctx); err != nil && ctx.Err() == nil {
			c.logger.WarnContext(ctx, "silent contained host check failed; retrying next interval", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// alertFor builds the alert for one silent contained host. Its subject names the containment, so the host alerts once for it and
// again only after it is released and contained anew.
func alertFor(h api.ContainedHost, silent time.Duration) api.Alert {
	minutes := int(silent / time.Minute)
	return api.Alert{
		HostID:      h.HostID,
		RuleID:      RuleID,
		Source:      api.AlertSourceDetection,
		Disposition: api.AlertDispositionAlert,
		Severity:    api.SeverityHigh,
		Title:       "Contained host stopped reporting",
		Description: "This host is contained but has not checked in for " + strconv.Itoa(minutes) + " minutes. The agent keeps a " +
			"contained host's network filter in place and reports any attempt to switch it off, so a contained host that falls " +
			"silent may have had its agent stopped, or may simply be asleep or powered off. Check on the host.",
		Subject: "containment:" + strconv.FormatInt(h.Epoch, 10),
	}
}
