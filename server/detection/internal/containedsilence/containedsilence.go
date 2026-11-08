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

// Check finds contained hosts that have stopped reporting and alerts on each, once per containment.
type Check struct {
	contained func(ctx context.Context) ([]api.ContainedHost, error)
	lastSeen  func(ctx context.Context, hostIDs []string) (map[string]int64, error)
	insert    Inserter
	after     time.Duration
	now       func() time.Time
	logger    *slog.Logger
}

// New builds a Check. A zero after means DefaultAfter, and a nil now means time.Now.
func New(contained func(ctx context.Context) ([]api.ContainedHost, error),
	lastSeen func(ctx context.Context, hostIDs []string) (map[string]int64, error), insert Inserter, after time.Duration,
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
	return &Check{contained: contained, lastSeen: lastSeen, insert: insert, after: after, now: now, logger: logger}
}

// Run raises an alert for every contained host silent for longer than the check's threshold, and reports how many were new.
//
// Silence is counted from the later of the host's last check-in and the moment it was contained. A host contained while already
// quiet gets the full threshold from the containment, so containing an offline laptop does not alert the moment the button is pressed.
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
	seen, err := c.lastSeen(ctx, ids)
	if err != nil {
		return 0, fmt.Errorf("read contained hosts' last check-in: %w", err)
	}
	now := c.now()
	created := 0
	for _, h := range hosts {
		quietSince := max(seen[h.HostID], h.ContainedAtNs)
		silent := now.Sub(time.Unix(0, quietSince))
		if silent <= c.after {
			continue
		}
		isNew, err := c.insert(ctx, alertFor(h, silent))
		if err != nil {
			return created, fmt.Errorf("alert on silent contained host %s: %w", h.HostID, err)
		}
		if isNew {
			created++
			c.logger.WarnContext(ctx, "contained host stopped reporting", "host_id", h.HostID,
				"silent_for", silent.Round(time.Second), "containment_epoch", h.Epoch)
		}
	}
	return created, nil
}

// Loop runs the check every interval until ctx is cancelled; a zero interval means DefaultInterval.
func (c *Check) Loop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := c.Run(ctx); err != nil {
				c.logger.WarnContext(ctx, "silent contained host check failed; retrying next interval", "err", err)
			}
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
