package health

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ComponentEventCapture is whether the agent has been keeping up with the events its sources deliver (issue #1584). A source hands
// each event to the agent over a bounded channel and drops it rather than block when the agent falls behind, since a blocked Endpoint
// Security client is killed. Before this the only record of such a drop was a local log line, so an operator never learned a host had
// lost telemetry.
const ComponentEventCapture = "event_capture"

const (
	// reasonEventsDropped is event capture within dropRecovery of its last drop.
	reasonEventsDropped = "events_dropped"
	// reasonNoRecentDrops is event capture once dropRecovery has passed without a drop. The component stays in the snapshot so the
	// count since the agent started is still there to read.
	reasonNoRecentDrops = "no_recent_drops"
)

// dropRecovery is how long after its last drop event capture reads degraded. Drops come in bursts (an install, a restart, a heavy
// build), so a condition that cleared the moment a burst ended would rarely be seen by anyone; an hour keeps a burst on the console
// long enough to be noticed, and then lets the host read healthy again.
const dropRecovery = time.Hour

// dropState is what the registry knows about dropped events since the agent started: how many each source dropped and when the last
// one was. Absent until the first drop, so a host that never dropped anything does not report the component at all.
type dropState struct {
	bySource map[string]int64
	// degradedSinceNs is when the current degraded spell began: the first drop after a quiet dropRecovery, or the first drop ever.
	degradedSinceNs int64
	lastNs          int64
}

// RecordDrop records one event that source, a registered component, dropped because the agent was not keeping up. It is called on the
// source's delivery path, once per dropped event, so it does only the bookkeeping. The change of status that a first drop, or a drop
// after a quiet hour, makes is reported promptly; later drops in the same spell wait for the next periodic report.
func (r *Registry) RecordDrop(source string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.nowNs()
	if r.drops == nil {
		r.drops = &dropState{bySource: map[string]int64{}}
	}
	if r.drops.lastNs == 0 || now-r.drops.lastNs >= int64(dropRecovery) {
		r.drops.degradedSinceNs = now
		r.notify()
	}
	r.drops.bySource[source]++
	r.drops.lastNs = now
}

// dropComponent renders the drops as the event capture component, or reports false when nothing has dropped. Whether it is degraded
// is decided here, from the clock, rather than by a timer, so recovery needs no goroutine: the next report after dropRecovery reads
// healthy, with the instant it recovered as its transition.
//
// Caller holds the lock.
func (r *Registry) dropComponent() (Component, bool) {
	d := r.drops
	if d == nil {
		return Component{}, false
	}
	counts := r.dropCounts()
	last := time.Unix(0, d.lastNs).UTC().Format(time.RFC3339)
	recoveredNs := d.lastNs + int64(dropRecovery)
	if r.nowNs() < recoveredNs {
		return Component{Type: ComponentEventCapture, Status: StatusDegraded, Reason: reasonEventsDropped,
			Message:          fmt.Sprintf("Dropped %s since the agent started, the last at %s, because the agent fell behind", counts, last),
			LastTransitionNs: d.degradedSinceNs}, true
	}
	return Component{Type: ComponentEventCapture, Status: StatusHealthy, Reason: reasonNoRecentDrops,
		Message:          fmt.Sprintf("No events dropped in the last hour. Dropped %s since the agent started, the last at %s", counts, last),
		LastTransitionNs: recoveredNs}, true
}

// dropCounts names each source's drops, by its display name, in a stable order: "1971 events from the Security extension and 12
// from the Network extension".
//
// Caller holds the lock.
func (r *Registry) dropCounts() string {
	sources := make([]string, 0, len(r.drops.bySource))
	for source := range r.drops.bySource {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	parts := make([]string, 0, len(sources))
	for i, source := range sources {
		name := source
		if s, ok := r.comps[source]; ok {
			name = s.displayName
		}
		if i == 0 {
			noun := "events"
			if r.drops.bySource[source] == 1 {
				noun = "event"
			}
			parts = append(parts, fmt.Sprintf("%d %s from the %s", r.drops.bySource[source], noun, name))
			continue
		}
		parts = append(parts, fmt.Sprintf("%d from the %s", r.drops.bySource[source], name))
	}
	return strings.Join(parts, " and ")
}
