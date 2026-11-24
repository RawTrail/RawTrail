package health

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pulsed reports whether the registry signalled a change, draining the signal.
func pulsed(r *Registry) bool {
	select {
	case <-r.Changed():
		return true
	default:
		return false
	}
}

// eventCapture returns the snapshot's event capture component, if any.
func eventCapture(r *Registry) (Component, bool) {
	for _, c := range r.Snapshot() {
		if c.Type == ComponentEventCapture {
			return c, true
		}
	}
	return Component{}, false
}

// spec:agent-status-reporting/dropped-events-are-reported-in-health/a-burst-the-agent-could-not-keep-up-with-is-reported
func TestRegistry_ReportsDroppedEvents(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 14, 17, 0, 0, 0, time.UTC).UnixNano()
	now := start
	r := newRegistryWithClock(func() int64 { return now })
	r.Register(ComponentEndpointSecurityExtension, "Security extension")
	r.Register(ComponentNetworkExtension, "Network extension")
	pulsed(r) // registration is not under test

	_, present := eventCapture(r)
	assert.False(t, present, "a host that dropped nothing does not report event capture at all")

	r.RecordDrop(ComponentEndpointSecurityExtension)
	assert.True(t, pulsed(r), "the first drop is reported promptly")
	now += int64(time.Minute)
	r.RecordDrop(ComponentEndpointSecurityExtension)
	r.RecordDrop(ComponentNetworkExtension)
	assert.False(t, pulsed(r), "later drops in the same spell wait for the periodic report")

	c, present := eventCapture(r)
	require.True(t, present)
	assert.Equal(t, Component{Type: ComponentEventCapture, Status: StatusDegraded, Reason: reasonEventsDropped, LastTransitionNs: start,
		Message: "Dropped 2 events from the Security extension and 1 from the Network extension since the agent started, the last at " +
			"2026-11-14T17:01:00Z, because the agent fell behind"}, c)

	// spec:agent-status-reporting/dropped-events-are-reported-in-health/event-capture-recovers-after-a-quiet-hour
	now = start + int64(time.Minute) + int64(dropRecovery)
	c, _ = eventCapture(r)
	assert.Equal(t, Component{Type: ComponentEventCapture, Status: StatusHealthy, Reason: reasonNoRecentDrops, LastTransitionNs: now,
		Message: "No events dropped in the last hour. Dropped 2 events from the Security extension and 1 from the Network extension " +
			"since the agent started, the last at 2026-11-14T17:01:00Z"}, c, "an hour without a drop reads healthy, keeping the count")

	now += int64(time.Minute)
	r.RecordDrop(ComponentNetworkExtension)
	assert.True(t, pulsed(r), "a drop after a quiet hour is a new spell, reported promptly")
	c, _ = eventCapture(r)
	assert.Equal(t, StatusDegraded, c.Status)
	assert.Equal(t, now, c.LastTransitionNs, "the new spell begins at its first drop")
	assert.Contains(t, c.Message, "2 from the Network extension", "and the count since the agent started carries on")
}
