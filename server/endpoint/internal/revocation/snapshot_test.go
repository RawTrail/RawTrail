package revocation

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type fakeSource struct {
	entries []Entry
	err     error
}

func (f *fakeSource) RevocationEntries(context.Context) ([]Entry, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.entries, nil
}

// TestNewSnapshot_ReportsMetrics pins the host snapshot's three instruments, which are what tell an operator the snapshot is live:
// its size, its age, and a counter of failed refreshes. The snapshot logic itself is tested in internal/revocationset.
func TestNewSnapshot_ReportsMetrics(t *testing.T) { //nolint:paralleltest // installs the process-global OTel meter provider
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider) // NewSnapshot resolves the global provider when it registers its instruments.
	t.Cleanup(func() {
		otel.SetMeterProvider(previous)
		_ = provider.Shutdown(context.Background())
	})

	src := &fakeSource{entries: []Entry{{ID: "revoked-host", Revoked: true}, {ID: "cycled-host", Epoch: 2}}}
	s := NewSnapshot(src, nil)
	require.NoError(t, s.Refresh(t.Context()))
	assert.False(t, s.Allowed("revoked-host", 0))
	src.err = errors.New("db down")
	require.Error(t, s.Refresh(t.Context()))

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	got := map[string]metricdata.Aggregation{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			got[m.Name] = m.Data
		}
	}

	size, ok := got["edr.auth.revocation_snapshot.size"].(metricdata.Gauge[int64])
	require.True(t, ok, "size gauge registered; collected %v", got)
	require.Len(t, size.DataPoints, 1)
	assert.Equal(t, int64(2), size.DataPoints[0].Value, "the failed refresh kept the two-host view")

	age, ok := got["edr.auth.revocation_snapshot.age_seconds"].(metricdata.Gauge[float64])
	require.True(t, ok, "age gauge registered; collected %v", got)
	require.Len(t, age.DataPoints, 1)
	assert.GreaterOrEqual(t, age.DataPoints[0].Value, 0.0)

	failures, ok := got["edr.auth.revocation_snapshot.refresh_failures"].(metricdata.Sum[int64])
	require.True(t, ok, "refresh failure counter registered; collected %v", got)
	require.Len(t, failures.DataPoints, 1)
	assert.Equal(t, int64(1), failures.DataPoints[0].Value)
}
