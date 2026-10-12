//go:build integration

package intake_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fleetdm/edr/server/detection/api"
	"github.com/fleetdm/edr/server/detection/internal/intake"
	"github.com/fleetdm/edr/server/detection/internal/mysql"
	detectiontestkit "github.com/fleetdm/edr/server/detection/testkit"
	endpointapi "github.com/fleetdm/edr/server/endpoint/api"
	"github.com/fleetdm/edr/server/testdb/full"
	visibilityapi "github.com/fleetdm/edr/server/visibility/api"
)

// acceptingEventLog accepts every append whatever its context, so a request whose context is already cancelled gets past the
// fan-out and reaches the best-effort MySQL writes after it, which are what these tests are about.
type acceptingEventLog struct{ visibilityapi.EventLog }

func (acceptingEventLog) Append(context.Context, []api.Event) error { return nil }

// TestIngest_DisconnectDuringBestEffortWrites pins that a disconnect cancelling the heartbeat bump or the host upsert, which run
// after the fan-out against the real store, is logged at INFO and answered with nothing, like a disconnect during the fan-out
// (issue #1291). Without the early return the handler would go on to write a 200 that nobody reads.
// spec:observability-instrumentation/ingest-disconnects-are-not-errors/a-disconnect-is-info-logged
func TestIngest_DisconnectDuringBestEffortWrites(t *testing.T) {
	t.Parallel()
	store, err := mysql.New(full.Open(t), detectiontestkit.NewMemArchive(), nil)
	require.NoError(t, err)

	const fork = `{"event_id":"e-fork","host_id":"host-a","timestamp_ns":1000,"event_type":"fork","payload":{"child_pid":42,"parent_pid":1}}`
	const heartbeat = `{"event_id":"e-hb","host_id":"host-a","timestamp_ns":1001,"event_type":"snapshot_heartbeat","payload":{"pid":42}}`
	cases := []struct {
		name    string
		body    string
		wantMsg string
		// skipped is a later write the handler must not attempt once it knows the agent has gone.
		skipped string
	}{
		{
			name:    "heartbeat bump",
			body:    "[" + fork + "," + heartbeat + "]",
			wantMsg: "heartbeat freshness bump: the agent disconnected",
			skipped: "upsert hosts",
		},
		{name: "host upsert", body: "[" + fork + "]", wantMsg: "upsert hosts: the agent disconnected"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			h := intake.New(store, logger, intake.BuildInfo{}, acceptingEventLog{}, detectiontestkit.NewMemArchive())

			ctx, cancel := context.WithCancel(endpointapi.WithHostID(context.Background(), "host-a"))
			cancel()
			req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/events", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			h.IngestHandler().ServeHTTP(rec, req)

			assert.Contains(t, logs.String(), "level=INFO msg=\""+tc.wantMsg)
			assert.NotContains(t, logs.String(), "level=ERROR", "a disconnect is not a server fault")
			assert.Zero(t, rec.Body.Len(), "nobody is left to read a response, so none is written")
			if tc.skipped != "" {
				assert.NotContains(t, logs.String(), tc.skipped, "the handler stops at the first write the disconnect cancelled")
			}
		})
	}
}
