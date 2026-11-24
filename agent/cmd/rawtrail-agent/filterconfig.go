package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/rawtrail/rawtrail/agent/selfheal"
)

// filterConfigInterval is how often a contained host's filter configuration is read. It matches the self-heal's grace window, so a
// filter orphaned this way is repaired about as soon as one that reported its stop.
const filterConfigInterval = 30 * time.Second

// providerObserver hands observeProviderStatus the extension's latest provider report together with the agent's latest reading of the
// filter configuration. The receiver loop and the configuration watch run on different goroutines, and health, the self-heal and the
// transition record each assume one ordered sequence of reports, so every observation is made under one lock.
type providerObserver struct {
	params receiverLoopParams

	mu        sync.Mutex
	last      providerStatus
	seen      bool
	filterOff bool
}

// sessionEnded forgets the last report, which described a session that has ended, so a later reading cannot replay its running
// providers over the disconnect health records. A reading still applies to the next session's first report, which the extension
// sends on every connect.
func (o *providerObserver) sessionEnded() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seen = false
}

// report observes a provider report from the extension.
func (o *providerObserver) report(ctx context.Context, status providerStatus) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.last, o.seen = status, true
	o.observeLocked(ctx)
}

// filterConfigRead records a reading of the filter configuration and returns whether it changed. A reading that changed is observed
// with the latest report, and so is every reading of a configuration switched off: an orphaned provider sends no further reports, and
// the self-heal repairs on the report after its grace window, so while the configuration reads off each reading stands in for one.
//
// With no report from the current session nothing is observed, and the reading applies to the first report. Health reads that window
// as awaiting providers or disconnected, never healthy, and a provider map made up here would overwrite it.
func (o *providerObserver) filterConfigRead(ctx context.Context, enabled bool) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	changed := o.filterOff == enabled
	o.filterOff = !enabled
	if o.seen && (changed || o.filterOff) {
		o.observeLocked(ctx)
	}
	return changed
}

func (o *providerObserver) observeLocked(ctx context.Context) {
	status := o.last
	status.FilterConfigOff = o.filterOff
	observeProviderStatus(ctx, o.params, status)
}

// watchFilterConfig reads the content filter's configuration every interval while the host is contained (issue #1540).
//
// The provider's own report cannot be trusted for this. Measured on edr-dev: a `disable-filter` landing milliseconds after the
// self-heal's `enable-filter` made nesessionmanager tear the filter session down while the provider was still starting, and it never
// called the provider's stopFilter. The provider went on reporting itself running, no flows reached it, the host was open, and with
// the configuration already off every later `disable-filter` changed nothing, so no further report came. Reading the configuration
// switched off puts the filter through the stopped path: health shows it, and the self-heal's enable-filter saves the configuration,
// which restarts the filter and re-contained the host within seconds.
//
// An uncontained host's configuration is not read, so it runs no host-app process. Its last reading is cleared, since it describes a
// containment that has ended.
func watchFilterConfig(ctx context.Context, obs *providerObserver, contained func() bool, reader selfheal.FilterConfigReader,
	interval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if !contained() {
			obs.filterConfigRead(ctx, true)
			continue
		}
		enabled, err := reader.FilterEnabled(ctx)
		if err != nil {
			if ctx.Err() == nil {
				logger.WarnContext(ctx, "could not read the content filter's configuration", "err", err)
			}
			continue
		}
		if obs.filterConfigRead(ctx, enabled) && !enabled {
			logger.WarnContext(ctx, "the content filter's configuration is switched off on a contained host; treating the filter as stopped")
		}
	}
}
