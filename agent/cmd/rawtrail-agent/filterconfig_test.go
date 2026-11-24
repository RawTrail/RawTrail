package main

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/agent/health"
	"github.com/rawtrail/rawtrail/agent/receiver"
	"github.com/rawtrail/rawtrail/agent/selfheal"
)

// filterConfigStub is a FilterConfigReader whose answer the test sets, counting its reads.
type filterConfigStub struct {
	mu      sync.Mutex
	enabled bool
	err     error
	reads   atomic.Int32
}

func (f *filterConfigStub) FilterEnabled(context.Context) (bool, error) {
	f.reads.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.enabled, f.err
}

func (f *filterConfigStub) set(enabled bool, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enabled, f.err = enabled, err
}

// contentFilterStatus is the content filter's status in the registry, or "" when none is reported.
func contentFilterStatus(registry *health.Registry) health.Status {
	for _, c := range registry.Snapshot() {
		if c.Type == health.ProviderContentFilter {
			return c.Status
		}
	}
	return ""
}

var bothRunning = map[string]string{health.ProviderContentFilter: health.ProviderRunning, health.ProviderDNSProxy: health.ProviderRunning}

// A contained host's switched-off filter configuration overrides the provider's own report (issue #1540).
func TestRequiredProviders_FilterConfigOff(t *testing.T) {
	t.Parallel()
	stopped := map[string]string{health.ProviderContentFilter: health.ProviderStopped, health.ProviderDNSProxy: health.ProviderRunning}
	for _, tc := range []struct {
		name            string
		contained       bool
		filterConfigOff bool
		want            map[string]string
	}{
		{"a contained host whose configuration is off", true, true, stopped},
		{"a contained host whose configuration is on", true, false, bothRunning},
		{"an uncontained host whose configuration is off", false, true, bothRunning},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			providers := maps.Clone(bothRunning)
			assert.Equal(t, tc.want, requiredProviders(providers, tc.contained, tc.filterConfigOff))
			assert.Equal(t, bothRunning, providers, "the report itself is not changed")
		})
	}
}

// spec:agent-status-reporting/a-contained-host-s-filter-configuration-is-checked/a-filter-orphaned-with-its-configuration-off-reads-stopped
//
// The edr-dev repro: the provider still reports itself running, but the configuration reads off. Health shows the filter stopped, the
// self-heal re-enables it after its grace window although no further report arrives, and once the configuration reads on again the
// report is read as sent.
func TestWatchFilterConfig_AnOrphanedFilterIsStoppedAndRepaired(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		registry := health.NewRegistry()
		registry.Register(health.ComponentNetworkExtension, "Network extension")
		remediator := &enableRecorder{}
		obs := &providerObserver{params: receiverLoopParams{
			health:    registry,
			component: health.ComponentNetworkExtension,
			contained: func() bool { return true },
			selfHeal:  selfheal.New(selfheal.Options{Remediator: remediator, Component: health.ComponentNetworkExtension}),
		}}
		obs.report(t.Context(), providerStatus{Providers: bothRunning, Decoded: true})
		require.Equal(t, health.StatusHealthy, contentFilterStatus(registry))

		reader := &filterConfigStub{}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go watchFilterConfig(ctx, obs, func() bool { return true }, reader, filterConfigInterval, slog.Default())

		time.Sleep(filterConfigInterval)
		synctest.Wait()
		assert.Equal(t, health.StatusUnhealthy, contentFilterStatus(registry), "a configuration that is off reads as the filter stopped")
		assert.Empty(t, remediator.snapshot(), "the self-heal waits out its grace window first")

		time.Sleep(filterConfigInterval)
		synctest.Wait()
		assert.Equal(t, []string{health.ProviderContentFilter}, remediator.snapshot(),
			"the next reading stands in for the report the orphaned provider never sends, so the self-heal repairs")

		reader.set(true, nil)
		time.Sleep(filterConfigInterval)
		synctest.Wait()
		assert.Equal(t, health.StatusHealthy, contentFilterStatus(registry), "a configuration back on reads the report as sent")
	})
}

// spec:agent-status-reporting/a-contained-host-s-filter-configuration-is-checked/an-uncontained-host-s-configuration-is-not-read
//
// An uncontained host runs no host-app process, and a reading taken while it was contained no longer applies once it is released.
func TestWatchFilterConfig_AnUncontainedHostIsNotRead(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		registry := health.NewRegistry()
		registry.Register(health.ComponentNetworkExtension, "Network extension")
		var contained atomic.Bool
		contained.Store(true)
		obs := &providerObserver{params: receiverLoopParams{health: registry, component: health.ComponentNetworkExtension,
			contained: contained.Load}}
		obs.report(t.Context(), providerStatus{Providers: bothRunning, Decoded: true})
		reader := &filterConfigStub{}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go watchFilterConfig(ctx, obs, contained.Load, reader, filterConfigInterval, slog.Default())

		time.Sleep(filterConfigInterval)
		synctest.Wait()
		require.Equal(t, int32(1), reader.reads.Load())
		require.Equal(t, health.StatusUnhealthy, contentFilterStatus(registry))

		contained.Store(false)
		time.Sleep(3 * filterConfigInterval)
		synctest.Wait()
		assert.Equal(t, int32(1), reader.reads.Load(), "the configuration is not read while uncontained")

		contained.Store(true)
		obs.report(t.Context(), providerStatus{Providers: bothRunning, Decoded: true})
		assert.Equal(t, health.StatusHealthy, contentFilterStatus(registry), "the reading from the earlier containment was cleared")
	})
}

// A reading that fails changes nothing: the last reading stands.
func TestWatchFilterConfig_AFailedReadChangesNothing(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		registry := health.NewRegistry()
		registry.Register(health.ComponentNetworkExtension, "Network extension")
		obs := &providerObserver{params: receiverLoopParams{health: registry, component: health.ComponentNetworkExtension,
			contained: func() bool { return true }}}
		obs.report(t.Context(), providerStatus{Providers: bothRunning, Decoded: true})
		reader := &filterConfigStub{err: errors.New("preferences daemon wedged")}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go watchFilterConfig(ctx, obs, func() bool { return true }, reader, filterConfigInterval, slog.Default())

		time.Sleep(2 * filterConfigInterval)
		synctest.Wait()
		assert.Equal(t, int32(2), reader.reads.Load())
		assert.Equal(t, health.StatusHealthy, contentFilterStatus(registry))
	})
}

// answerOnceReader reads the configuration as off once, then blocks until its context ends, so nothing but the extension's reports
// moves health after the first reading.
type answerOnceReader struct{ reads atomic.Int32 }

func (r *answerOnceReader) FilterEnabled(ctx context.Context) (bool, error) {
	if r.reads.Add(1) > 1 {
		<-ctx.Done()
		return false, ctx.Err()
	}
	return false, nil
}

// The wiring: the network extension loop runs the watch, and the extension's reports and the watch's readings reach health through one
// observer, so a running report that arrives after the configuration read off does not clear the stop. Deleting the watch from
// startReceiverLoop, or handing reports to observeProviderStatus past the observer, fails here.
func TestReceiverLoop_ChecksAContainedHostsFilterConfig(t *testing.T) {
	t.Parallel()
	registry := health.NewRegistry()
	registry.Register(health.ComponentNetworkExtension, "Network extension")
	status := []byte(`{"event_type":"ne_provider_status","payload":{"providers":{"content_filter":"running","dns_proxy":"running"}}}`)
	connector := &eventConnector{events: make(chan receiver.Event, 1), errs: make(chan int)}
	connector.events <- receiver.Event{Data: status}
	reader := &answerOnceReader{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		startReceiverLoop(ctx, receiverLoopParams{
			logger:            slog.Default(),
			serviceLabel:      "test-ne",
			health:            registry,
			component:         health.ComponentNetworkExtension,
			providerLiveness:  true,
			contained:         func() bool { return true },
			filterConfig:      reader,
			filterConfigEvery: 5 * time.Millisecond,
			connectorFactory:  func() receiver.Connector { return connector },
			enqueueBatch:      func(context.Context, [][]byte) error { return nil },
		})
	}()
	require.Eventually(t, func() bool { return contentFilterStatus(registry) == health.StatusUnhealthy }, 2*time.Second,
		5*time.Millisecond, "the switched-off configuration overrides the extension's running report")
	require.Eventually(t, func() bool { return reader.reads.Load() > 1 }, 2*time.Second, time.Millisecond,
		"the watch is now parked, so only a report can move health")

	connector.events <- receiver.Event{Data: status}
	require.Eventually(t, func() bool { return len(connector.events) == 0 }, 2*time.Second, time.Millisecond)
	assert.Never(t, func() bool { return contentFilterStatus(registry) != health.StatusUnhealthy }, 200*time.Millisecond,
		10*time.Millisecond, "a later running report is read with the configuration's reading")

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("startReceiverLoop did not return after its context ended")
	}
}

// networkExtensionComponent is the network extension's own component in the registry.
func networkExtensionComponent(t *testing.T, registry *health.Registry) health.Component {
	t.Helper()
	for _, c := range registry.Snapshot() {
		if c.Type == health.ComponentNetworkExtension {
			return c
		}
	}
	t.Fatal("no network extension component")
	return health.Component{}
}

// A reading taken before the session's first report is kept, not dropped: it applies to that report.
func TestProviderObserver_AReadingBeforeTheFirstReportAppliesToIt(t *testing.T) {
	t.Parallel()
	registry := health.NewRegistry()
	registry.Register(health.ComponentNetworkExtension, "Network extension")
	obs := &providerObserver{params: receiverLoopParams{health: registry, component: health.ComponentNetworkExtension,
		contained: func() bool { return true }}}

	obs.filterConfigRead(t.Context(), false)
	assert.Empty(t, contentFilterStatus(registry), "with no report there is no provider map to grade")

	obs.report(t.Context(), providerStatus{Providers: bothRunning, Decoded: true})
	assert.Equal(t, health.StatusUnhealthy, contentFilterStatus(registry), "the first report is read with the reading")
}

// After the session ends, a reading does not replay the ended session's running providers over the disconnect health recorded.
func TestProviderObserver_AReadingAfterTheSessionEndsReplaysNothing(t *testing.T) {
	t.Parallel()
	registry := health.NewRegistry()
	registry.Register(health.ComponentNetworkExtension, "Network extension")
	obs := &providerObserver{params: receiverLoopParams{health: registry, component: health.ComponentNetworkExtension,
		contained: func() bool { return true }}}
	obs.report(t.Context(), providerStatus{Providers: bothRunning, Decoded: true})
	obs.filterConfigRead(t.Context(), false)

	obs.sessionEnded()
	registry.MarkDisconnected(health.ComponentNetworkExtension)
	obs.filterConfigRead(t.Context(), true)

	ne := networkExtensionComponent(t, registry)
	assert.Equal(t, health.StatusUnhealthy, ne.Status)
	assert.Equal(t, "connection_lost", ne.Reason, "the disconnect stands")
}

// refusingConnector never connects, so the loop stays disconnected after the first session.
type refusingConnector struct{ eventConnector }

func (refusingConnector) Connect() error { return errors.New("extension unavailable") }

// The wiring: the loop tells the observer when its session ends, so a reading after a disconnect cannot mark the network extension
// healthy. Deleting the observer from the loop's OnDisconnected fails here.
func TestReceiverLoop_AReadingAfterADisconnectReplaysNothing(t *testing.T) {
	t.Parallel()
	registry := health.NewRegistry()
	registry.Register(health.ComponentNetworkExtension, "Network extension")
	status := []byte(`{"event_type":"ne_provider_status","payload":{"providers":{"content_filter":"running","dns_proxy":"running"}}}`)
	first := newEventConnector(status)
	var built atomic.Int32
	reader := &filterConfigStub{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		startReceiverLoop(ctx, receiverLoopParams{
			logger:            slog.Default(),
			serviceLabel:      "test-ne",
			health:            registry,
			component:         health.ComponentNetworkExtension,
			providerLiveness:  true,
			contained:         func() bool { return true },
			filterConfig:      reader,
			filterConfigEvery: 5 * time.Millisecond,
			connectorFactory: func() receiver.Connector {
				if built.Add(1) == 1 {
					return first
				}
				return &refusingConnector{}
			},
			enqueueBatch: func(context.Context, [][]byte) error { return nil },
		})
	}()
	require.Eventually(t, func() bool { return contentFilterStatus(registry) == health.StatusUnhealthy }, 2*time.Second,
		5*time.Millisecond)

	first.errs <- 1
	require.Eventually(t, func() bool { return networkExtensionComponent(t, registry).Reason == "connection_lost" }, 2*time.Second,
		5*time.Millisecond)
	reader.set(true, nil)
	assert.Never(t, func() bool { return networkExtensionComponent(t, registry).Status == health.StatusHealthy }, 200*time.Millisecond,
		10*time.Millisecond, "a reading after the disconnect does not replay the ended session's report")

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("startReceiverLoop did not return after its context ended")
	}
}
