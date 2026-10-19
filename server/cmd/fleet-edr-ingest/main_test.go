package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	detectionapi "github.com/rawtrail/rawtrail/server/detection/api"
)

// recorderSink stands in for the detection context: it keeps the recorder SetMetrics hands it.
type recorderSink struct{ got detectionapi.MetricsRecorder }

func (s *recorderSink) SetMetrics(m detectionapi.MetricsRecorder) { s.got = m }

// spec:observability-instrumentation/the-ingest-service-counts-the-events-it-accepts/events-accepted-by-the-ingest-service-are-counted
// spec:observability-instrumentation/the-ingest-service-counts-the-events-it-accepts/the-ingest-service-reports-no-host-fleet-gauges
//
// The ingest binary must hand detection a recorder, or its intake handler counts nothing (issue #1263), and must not register the
// host-fleet gauges, which fleet-edr-server already reports for the whole deployment.
func TestWireIngestMetrics(t *testing.T) { //nolint:paralleltest // installs the process-global OTel meter provider
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider) // metrics.New resolves the global provider when it registers its instruments.
	t.Cleanup(func() {
		otel.SetMeterProvider(previous)
		_ = provider.Shutdown(context.Background())
	})

	sink := &recorderSink{}
	rec := wireIngestMetrics(sink)
	require.Same(t, rec, sink.got, "detection gets the recorder the HTTP server is given")
	sink.got.EventsIngested(t.Context(), "host-a", map[string]int{"exec": 2})

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	names := map[string]bool{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			names[m.Name] = true
		}
	}
	assert.True(t, names["edr.events.ingested"], "accepted events are counted; collected %v", names)
	assert.False(t, names["edr.enrolled.hosts"], "no enrolled-hosts gauge from the ingest tier")
	assert.False(t, names["edr.offline.hosts"], "no offline-hosts gauge from the ingest tier")
	assert.False(t, names["edr.events.pending"], "no backlog gauge from the ingest tier: the server reports the shared queue")
}
