package mysql_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/internal/mysql"
	"github.com/rawtrail/rawtrail/server/detection/testkit"
	"github.com/rawtrail/rawtrail/server/testdb"
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
	visibilitytestkit "github.com/rawtrail/rawtrail/server/visibility/testkit"
)

// countingArchive records the ids each EventsByIDs call asks the archive for.
type countingArchive struct {
	visibilityapi.EventArchive
	mu    sync.Mutex
	asked [][]string
}

func (c *countingArchive) EventsByIDs(ctx context.Context, eventIDs []string) ([]visibilityapi.Event, error) {
	c.mu.Lock()
	c.asked = append(c.asked, eventIDs)
	c.mu.Unlock()
	return c.EventArchive.EventsByIDs(ctx, eventIDs)
}

// spec:server-detection-rules-engine/captured-alert-evidence-is-reused/captured-evidence-is-not-read-from-the-archive-again
//
// TestStore_InsertAlert_ReusesCapturedEvidence pins that evidence an alert already holds is copied from it, not read from the archive
// again. A batch replayed while it waits for a process is evaluated again and re-inserts each finding's alert: on dogfood replays ran
// about twice a second, and each re-read its findings' events from ClickHouse at some 550,000 rows apiece (issue #1550).
func TestStore_InsertAlert_ReusesCapturedEvidence(t *testing.T) {
	t.Parallel()
	db := testdb.Open(t)
	ctx := t.Context()
	require.NoError(t, testkit.ApplySchema(ctx, db))
	mem := visibilitytestkit.NewMemArchive()
	archive := &countingArchive{EventArchive: mem}
	s, err := mysql.New(db, archive, nil)
	require.NoError(t, err)
	require.NoError(t, mem.Insert(ctx, []api.Event{
		{EventID: "ev-1", HostID: "h1", TimestampNs: 100, EventType: "agent_credential_would_block", Payload: json.RawMessage(`{"pid":7}`)},
		{EventID: "ev-2", HostID: "h1", TimestampNs: 200, EventType: "agent_credential_would_block", Payload: json.RawMessage(`{"pid":8}`)},
	}))
	alert := func(subject string) api.Alert {
		return api.Alert{HostID: "h1", RuleID: "test_rule", Severity: api.SeverityHigh, Title: "t", Description: "d", Subject: subject}
	}

	first, created, err := s.InsertAlert(ctx, alert("test:first"), []string{"ev-1"})
	require.NoError(t, err)
	require.True(t, created)

	// A replay of the same finding reads nothing from the archive.
	archive.asked = nil
	again, created, err := s.InsertAlert(ctx, alert("test:first"), []string{"ev-1"})
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, first, again)
	assert.Empty(t, archive.asked, "a replay reads nothing from the archive")

	// Another alert takes the captured copy and reads only what no alert holds.
	archive.asked = nil
	second, created, err := s.InsertAlert(ctx, alert("test:second"), []string{"ev-1", "ev-2"})
	require.NoError(t, err)
	require.True(t, created)
	assert.Equal(t, [][]string{{"ev-2"}}, archive.asked, "only the event no alert holds is read")
	evidence, err := s.GetAlertEventPayloads(ctx, second)
	require.NoError(t, err)
	require.Len(t, evidence, 2)
	assert.Equal(t, []string{"ev-1", "ev-2"}, []string{evidence[0].EventID, evidence[1].EventID})
	assert.JSONEq(t, `{"pid":7}`, string(evidence[0].Payload))
	assert.Equal(t, "agent_credential_would_block", evidence[0].EventType)
	assert.Equal(t, int64(100), evidence[0].TimestampNs)

	// A monitor record takes the captured copy too.
	archive.asked = nil
	require.NoError(t, s.InsertMonitorRecords(ctx, []mysql.MonitorRecord{monitorRecord("test:monitor", "ev-1")}))
	assert.Empty(t, archive.asked, "a monitor record reads nothing from the archive")
	records, err := s.ListAlerts(ctx, api.AlertFilter{HostID: "h1", Disposition: api.AlertDispositionMonitor, Limit: 10})
	require.NoError(t, err)
	require.Len(t, records, 1)
	evidence, err = s.GetAlertEventPayloads(ctx, records[0].ID)
	require.NoError(t, err)
	require.Len(t, evidence, 1)
	assert.JSONEq(t, `{"pid":7}`, string(evidence[0].Payload))
}
