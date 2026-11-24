package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/rawtrail/rawtrail/agent/health"
	"github.com/rawtrail/rawtrail/agent/metrics"
)

// A receiver's dropped event is counted in the agent's health, against its source, and as a metric (issue #1584).
func TestTelemetryDeps_DropReporterCountsInHealthAndMetric(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	registry := health.NewRegistry()
	registry.Register(health.ComponentEndpointSecurityExtension, "Security extension")
	d := telemetryDeps{health: registry, metrics: metrics.NewWithMeter(nil, mp.Meter("test"))}

	d.dropReporter(t.Context(), health.ComponentEndpointSecurityExtension)()

	var capture *health.Component
	for _, c := range registry.Snapshot() {
		if c.Type == health.ComponentEventCapture {
			capture = &c
		}
	}
	require.NotNil(t, capture, "the drop is in the agent's health")
	assert.Contains(t, capture.Message, "1 event from the Security extension")

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	var counted int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok && m.Name == "edr.agent.receiver.dropped" {
				for _, dp := range sum.DataPoints {
					counted += dp.Value
				}
			}
		}
	}
	assert.Equal(t, int64(1), counted, "and in the metric")
}
