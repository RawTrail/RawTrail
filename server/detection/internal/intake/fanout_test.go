package intake

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
	endpointapi "github.com/rawtrail/rawtrail/server/endpoint/api"
	"github.com/rawtrail/rawtrail/server/httpserver"
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// fakeEventArchive records the events handed to Insert and can be made to fail, so the fan-out's archive-first ordering and its
// error handling are testable without a ClickHouse container (the in-memory MemArchive used elsewhere never fails).
type fakeEventArchive struct {
	inserted  [][]api.Event
	insertErr error
}

func (f *fakeEventArchive) Insert(_ context.Context, events []api.Event) error {
	f.inserted = append(f.inserted, events)
	return f.insertErr
}

func (f *fakeEventArchive) NetworkEventsForProcess(context.Context, string, int, httpserver.TimeRange) ([]api.Event, error) {
	return nil, nil
}
func (f *fakeEventArchive) NetworkEventsForGeneration(context.Context, api.ProcessFlowFilter) ([]api.Event, bool, error) {
	return nil, false, nil
}
func (f *fakeEventArchive) EventsByTypeForHost(context.Context, string, string, httpserver.TimeRange) ([]api.Event, error) {
	return nil, nil
}
func (f *fakeEventArchive) EventsByIDs(context.Context, []string) ([]api.Event, error) {
	return nil, nil
}

func (f *fakeEventArchive) TelemetryActivityForHosts(
	context.Context, []string, httpserver.TimeRange,
) (map[string]api.TelemetryActivity, error) {
	return nil, nil
}

func (f *fakeEventArchive) SearchEvents(context.Context, api.EventSearchFilter, string, int) (api.EventSearchResult, error) {
	return api.EventSearchResult{}, nil
}

func (f *fakeEventArchive) HostTimeline(context.Context, api.HostTimelineFilter, string, int) (api.EventSearchResult, error) {
	return api.EventSearchResult{}, nil
}

func (f *fakeEventArchive) ActivityForGenerations(context.Context, api.ActivityFilter) (api.ActivityRollup, error) {
	return api.ActivityRollup{}, nil
}

func (f *fakeEventArchive) SessionTouches(context.Context, string, visibilityapi.ProcessGeneration) ([]visibilityapi.SessionTouch, error) {
	return nil, nil
}

func (f *fakeEventArchive) AgentToolCalls(context.Context, visibilityapi.AgentToolCallFilter) ([]visibilityapi.AgentToolCall, bool, error) {
	return nil, false, nil
}

func (f *fakeEventArchive) ActivityTotalsForGenerations(context.Context, api.ActivityFilter) (visibilityapi.ActivitySummary, error) {
	return visibilityapi.ActivitySummary{}, nil
}

func (f *fakeEventArchive) AgentToolCallCounts(context.Context, visibilityapi.AgentToolCallFilter) ([]visibilityapi.ToolCallCount, error) {
	return nil, nil
}

func (f *fakeEventArchive) AgentSessionLineage(context.Context, string, string, int64) (visibilityapi.AgentSessionLineage, error) {
	return visibilityapi.AgentSessionLineage{}, nil
}

// fakeEventLog records the events handed to Append and can be made to fail.
type fakeEventLog struct {
	appended  [][]api.Event
	appendErr error
}

func (f *fakeEventLog) Append(_ context.Context, events []api.Event) error {
	f.appended = append(f.appended, events)
	return f.appendErr
}
func (f *fakeEventLog) PendingHosts(context.Context, int) ([]string, error) { return nil, nil }
func (f *fakeEventLog) ClaimForHost(context.Context, string, int) ([]api.Event, int64, error) {
	return nil, 0, nil
}
func (f *fakeEventLog) Ack(context.Context, []string, int64) (bool, error) { return true, nil }
func (f *fakeEventLog) Nack(context.Context, []string, int64, []byte) (visibilityapi.NackResult, error) {
	return visibilityapi.NackResult{Held: true}, nil
}
func (f *fakeEventLog) CountPending(context.Context) (int64, error)            { return 0, nil }
func (f *fakeEventLog) PruneProcessed(context.Context, int) (int64, error)     { return 0, nil }
func (f *fakeEventLog) PruneSetAside(context.Context, int, int) (int64, error) { return 0, nil }

// postBatch drives handleIngest directly with host_id pinned the way the HostToken middleware does, so these tests need no DB: the
// store (host-summary + snapshot-freshness writes) is only reached after a successful fan-out, which the error-path cases never hit.
func postBatch(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	ctx := endpointapi.WithHostID(context.Background(), "host-a")
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/events", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.IngestHandler().ServeHTTP(rec, req)
	return rec
}

// TestHandleIngest_FanOut pins the ADR-0015 ingest fan-out: archive first, then the work queue, 200 only after both, and a 500 (never
// a partial enqueue) if either store fails. The happy path is covered end-to-end by the integration suite; these cases pin the
// ordering and the two failure branches that an in-memory archive cannot exercise.
// spec:server-event-ingestion/decoupled-processing-pipeline/an-accepted-batch-is-both-archived-and-enqueued
func TestHandleIngest_FanOut(t *testing.T) {
	t.Parallel()
	// A fork (stored) plus a snapshot_heartbeat (partitioned out before the fan-out, issue #408), so we can assert the stores see
	// only the storable event.
	const body = `[` +
		`{"event_id":"e-fork","host_id":"host-a","timestamp_ns":1000,"event_type":"fork","payload":{"child_pid":42,"parent_pid":1}},` +
		`{"event_id":"e-hb","host_id":"host-a","timestamp_ns":1001,"event_type":"snapshot_heartbeat","payload":{"pid":42}}` +
		`]`

	t.Run("archive insert failure returns 500 and does not enqueue", func(t *testing.T) {
		t.Parallel()
		archive := &fakeEventArchive{insertErr: errors.New("archive down")}
		queue := &fakeEventLog{}
		h := New(nil, nil, BuildInfo{}, queue, archive)

		rec := postBatch(t, h, body)
		assert.Equal(t, http.StatusInternalServerError, rec.Code, "an archive write failure must not be acknowledged")
		assert.Empty(t, queue.appended, "archive-first: the queue must not be appended when the archive write fails")
	})

	t.Run("eventlog append failure returns 500 after archiving", func(t *testing.T) {
		t.Parallel()
		archive := &fakeEventArchive{}
		queue := &fakeEventLog{appendErr: errors.New("queue down")}
		h := New(nil, nil, BuildInfo{}, queue, archive)

		rec := postBatch(t, h, body)
		assert.Equal(t, http.StatusInternalServerError, rec.Code, "a queue write failure must not be acknowledged")
		require.Len(t, archive.inserted, 1, "the archive is written first, before the queue append is attempted")
		require.Len(t, archive.inserted[0], 1, "heartbeats are partitioned out before the fan-out; only the fork is stored")
		assert.Equal(t, "e-fork", archive.inserted[0][0].EventID)
	})

	t.Run("spec:server-event-ingestion/platform-tagged-event-envelope/platform-survives-the-queue-to-rule-evaluation", func(t *testing.T) {
		t.Parallel()
		archive := &fakeEventArchive{}
		queue := &fakeEventLog{}
		h := New(nil, nil, BuildInfo{}, queue, archive)

		// A windows event and a legacy event with no platform. The engine claims from the queue, so the platform the queue receives is
		// the platform rule evaluation sees: the windows value passes through and the absent one is normalized to darwin at intake.
		const platformBody = `[` +
			`{"event_id":"e-win","host_id":"host-a","timestamp_ns":1000,"event_type":"exec","platform":"windows","payload":{}},` +
			`{"event_id":"e-legacy","host_id":"host-a","timestamp_ns":1001,"event_type":"exec","payload":{}}` +
			`]`
		// Fail the queue append so the store (host-summary) write is never reached, but only after the queue captured the events.
		queue.appendErr = errors.New("stop before the store write")
		rec := postBatch(t, h, platformBody)
		require.Equal(t, http.StatusInternalServerError, rec.Code)
		require.Len(t, queue.appended, 1)
		require.Len(t, queue.appended[0], 2)
		byID := map[string]string{
			queue.appended[0][0].EventID: queue.appended[0][0].Platform,
			queue.appended[0][1].EventID: queue.appended[0][1].Platform,
		}
		assert.Equal(t, "windows", byID["e-win"], "a windows event reaches the queue tagged windows")
		assert.Equal(t, api.PlatformDarwin, byID["e-legacy"], "an event with no platform is normalized to darwin before the queue")
	})

	t.Run("ingested_at_ns is server-stamped, not agent-controlled", func(t *testing.T) {
		t.Parallel()
		archive := &fakeEventArchive{}
		queue := &fakeEventLog{}
		h := New(nil, nil, BuildInfo{}, queue, archive)

		// The agent supplies a bogus far-future ingested_at_ns; the server must overwrite it with its own arrival clock.
		before := time.Now().UnixNano()
		const agentSpoofed = int64(1) << 62
		spoofed := `[{"event_id":"e-spoof","host_id":"host-a","timestamp_ns":1000,"ingested_at_ns":` +
			strconv.FormatInt(agentSpoofed, 10) + `,"event_type":"fork","payload":{"child_pid":42,"parent_pid":1}}]`
		// The store is reached on the happy path (host-summary write), so this case needs a store; assert via the archive instead by
		// failing the queue after the archive captured the stamped event.
		queue.appendErr = errors.New("stop before the store write")
		rec := postBatch(t, h, spoofed)
		require.Equal(t, http.StatusInternalServerError, rec.Code)
		require.Len(t, archive.inserted, 1)
		require.Len(t, archive.inserted[0], 1)
		got := archive.inserted[0][0].IngestedAtNs
		assert.NotEqual(t, agentSpoofed, got, "the agent-supplied ingested_at_ns must be overwritten")
		assert.GreaterOrEqual(t, got, before, "ingested_at_ns is stamped from the server clock at ingest")
	})
}

// TestHandleIngest_StoreFailureLogging pins how a failed fan-out write is logged and answered (issue #1291). A write the agent's own
// disconnect cancelled is INFO with no response, because the agent resends the unacknowledged batch and nobody is left to read one.
// Every other failure is an ERROR and a 500, including a cancellation while the request is still live.
func TestHandleIngest_StoreFailureLogging(t *testing.T) {
	t.Parallel()
	const body = `[{"event_id":"e-fork","host_id":"host-a","timestamp_ns":1000,"event_type":"fork","payload":{"child_pid":42,"parent_pid":1}}]`
	cases := []struct {
		name          string
		archiveErr    error
		appendErr     error
		disconnected  bool
		wantLevel     string
		wantMsg       string
		wantResponded bool
	}{
		{
			name:         "spec:observability-instrumentation/ingest-disconnects-are-not-errors/a-disconnect-is-info-logged",
			appendErr:    context.Canceled,
			disconnected: true,
			wantLevel:    "level=INFO",
			wantMsg:      "eventlog append error: the agent disconnected",
		},
		{
			name:         "archive insert cancelled by a disconnect is info-logged",
			archiveErr:   fmt.Errorf("insert: %w", context.Canceled),
			disconnected: true,
			wantLevel:    "level=INFO",
			wantMsg:      "archive insert error: the agent disconnected",
		},
		{
			name:          "spec:observability-instrumentation/ingest-disconnects-are-not-errors/a-live-cancellation-stays-an-error",
			appendErr:     context.Canceled,
			wantLevel:     "level=ERROR",
			wantMsg:       `msg="eventlog append error"`,
			wantResponded: true,
		},
		{
			name:          "spec:observability-instrumentation/ingest-disconnects-are-not-errors/a-write-failure-stays-an-error",
			appendErr:     errors.New("queue down"),
			disconnected:  true,
			wantLevel:     "level=ERROR",
			wantMsg:       `msg="eventlog append error"`,
			wantResponded: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			h := New(nil, logger, BuildInfo{}, &fakeEventLog{appendErr: tc.appendErr}, &fakeEventArchive{insertErr: tc.archiveErr})

			ctx, cancel := context.WithCancel(endpointapi.WithHostID(context.Background(), "host-a"))
			defer cancel()
			if tc.disconnected {
				cancel()
			}
			req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/events", strings.NewReader(body))
			rec := httptest.NewRecorder()
			h.IngestHandler().ServeHTTP(rec, req)

			assert.Contains(t, logs.String(), tc.wantLevel)
			assert.Contains(t, logs.String(), tc.wantMsg)
			if tc.wantResponded {
				assert.Equal(t, http.StatusInternalServerError, rec.Code)
				return
			}
			assert.NotContains(t, logs.String(), "level=ERROR", "a disconnect is not a server fault")
			assert.Zero(t, rec.Body.Len(), "nobody is left to read a response, so none is written")
			assert.Empty(t, rec.Header().Get("Content-Type"), "no error response is started")
		})
	}
}
