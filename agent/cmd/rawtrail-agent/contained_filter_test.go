package main

import (
	"context"
	"maps"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/agent/health"
	"github.com/rawtrail/rawtrail/agent/selfheal"
	"github.com/rawtrail/rawtrail/agent/sensorevent"
)

// spec:agent-status-reporting/a-contained-host-s-content-filter-is-required/an-uncontained-host-s-missing-filter-is-not-a-fault
//
// A contained host's content filter is required: a report that does not name it reads as the filter stopped (issue #1540).
func TestRequiredProviders(t *testing.T) {
	t.Parallel()
	dnsOnly := map[string]string{health.ProviderDNSProxy: health.ProviderRunning}
	for _, tc := range []struct {
		name      string
		providers map[string]string
		contained bool
		want      map[string]string
	}{
		{"a contained host whose filter is missing", dnsOnly, true,
			map[string]string{health.ProviderDNSProxy: health.ProviderRunning, health.ProviderContentFilter: health.ProviderStopped}},
		{"an uncontained host whose filter is missing", dnsOnly, false, dnsOnly},
		{"a contained host whose filter is reported", map[string]string{health.ProviderContentFilter: health.ProviderRunning}, true,
			map[string]string{health.ProviderContentFilter: health.ProviderRunning}},
		{"a contained host whose filter was disabled on purpose", map[string]string{health.ProviderContentFilter: health.ProviderDisabled},
			true, map[string]string{health.ProviderContentFilter: health.ProviderDisabled}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			before := maps.Clone(tc.providers)
			assert.Equal(t, tc.want, requiredProviders(tc.providers, tc.contained))
			assert.Equal(t, before, tc.providers, "the report itself is not changed")
		})
	}
}

// spec:agent-status-reporting/a-contained-host-s-content-filter-is-required/a-contained-host-whose-filter-never-starts-reads-unhealthy
//
// On a contained host whose filter configuration is off, the extension reports only its DNS proxy. Health then shows the content
// filter stopped and the network extension unhealthy, where it read healthy before.
func TestObserveProviderStatus_AContainedHostWithoutItsFilterIsUnhealthy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		contained  bool
		decoded    bool
		wantStatus health.Status
		wantFilter bool
	}{
		{"contained", true, true, health.StatusUnhealthy, true},
		{"not contained", false, true, health.StatusHealthy, false},
		{"contained, but the report did not decode", true, false, health.StatusHealthy, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			registry := health.NewRegistry()
			registry.Register(health.ComponentNetworkExtension, "Network extension")
			p := receiverLoopParams{health: registry, component: health.ComponentNetworkExtension,
				contained: func() bool { return tc.contained }}

			observeProviderStatus(context.Background(), p, providerStatus{
				Providers: map[string]string{health.ProviderDNSProxy: health.ProviderRunning}, Decoded: tc.decoded})

			snapshot := registry.Snapshot()
			require.NotEmpty(t, snapshot)
			assert.Equal(t, tc.wantStatus, snapshot[0].Status)
			var filter *health.Component
			for _, c := range snapshot {
				if c.Type == health.ProviderContentFilter {
					filter = &c
				}
			}
			if !tc.wantFilter {
				assert.Nil(t, filter, "no content filter is claimed")
				return
			}
			require.NotNil(t, filter, "the content filter is reported")
			assert.Equal(t, health.StatusUnhealthy, filter.Status)
		})
	}
}

// enableRecorder is a self-heal remediator that records what it was asked to re-enable.
type enableRecorder struct {
	mu      sync.Mutex
	enabled []string
}

func (e *enableRecorder) Enable(_ context.Context, provider string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.enabled = append(e.enabled, provider)
	return nil
}

func (e *enableRecorder) snapshot() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.enabled...)
}

// spec:agent-status-reporting/a-contained-host-s-content-filter-is-required/a-contained-host-whose-filter-never-starts-reads-unhealthy
//
// The missing filter goes through the self-heal and the tamper record too: the self-heal re-enables it, and a filter that was running
// and then went missing is recorded as a stop.
func TestObserveProviderStatus_AContainedHostsMissingFilterIsRepairedAndRecorded(t *testing.T) {
	t.Parallel()
	remediator := &enableRecorder{}
	var mu sync.Mutex
	var transitions []map[string]any
	emit := func(_ context.Context, _ string, payload map[string]any) error {
		mu.Lock()
		defer mu.Unlock()
		transitions = append(transitions, payload)
		return nil
	}
	p := receiverLoopParams{
		component: health.ComponentNetworkExtension,
		contained: func() bool { return true },
		selfHeal: selfheal.New(selfheal.Options{Remediator: remediator, Component: health.ComponentNetworkExtension,
			Grace: time.Nanosecond}),
		transitions: sensorevent.New(emit, nil),
	}
	running := map[string]string{health.ProviderContentFilter: health.ProviderRunning, health.ProviderDNSProxy: health.ProviderRunning}
	observeProviderStatus(t.Context(), p, providerStatus{Providers: running, Decoded: true})
	missing := providerStatus{Providers: map[string]string{health.ProviderDNSProxy: health.ProviderRunning}, Decoded: true}
	observeProviderStatus(t.Context(), p, missing) // the first report of the stop starts the self-heal's grace window
	time.Sleep(time.Millisecond)
	observeProviderStatus(t.Context(), p, missing) // and the next one after it repairs

	require.Eventually(t, func() bool { return len(remediator.snapshot()) > 0 }, 2*time.Second, 5*time.Millisecond)
	assert.Equal(t, health.ProviderContentFilter, remediator.snapshot()[0], "the self-heal re-enables the filter")
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, transitions, 1, "the filter going missing is recorded as one transition")
	assert.Equal(t, health.ProviderContentFilter, transitions[0]["provider"])
	assert.Equal(t, health.ProviderStopped, transitions[0]["state"])
}
