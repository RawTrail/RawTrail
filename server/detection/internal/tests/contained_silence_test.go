//go:build integration

package tests

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/internal/containedsilence"
)

// spec:server-host-status/a-contained-host-that-stops-reporting-raises-an-alert/a-contained-host-silent-past-the-threshold-raises-one-alert
// spec:server-host-status/a-contained-host-that-stops-reporting-raises-an-alert/a-new-containment-alerts-again
//
// TestContainedSilence_OneAlertPerContainmentInTheRealStore runs the check against MySQL (issue #1228). "Once per containment" rests on
// two things there: the host's record of the containment it alerted for, which outlives alert retention, and the alerts table's dedup
// key, which keeps two replicas running the check without a lock from raising it twice. Silence is read from the server's own
// check-ins, never from event timestamps the agent's clock wrote.
func TestContainedSilence_OneAlertPerContainmentInTheRealStore(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store, db := openProcessStore(t)
	now := time.Now()
	// The silent host's events were stamped a day ahead by its own clock, which ingest's last_seen_ns takes as given. Only the
	// server's check-ins count, so it is silent all the same. The busy host checks in through the server's heartbeat.
	// The busy host was quiet once too: its heartbeat has to move an existing row's check-in forward, not just set a new row's.
	_, err := db.ExecContext(ctx, `INSERT INTO hosts (host_id, event_count, last_seen_ns, last_checkin_ns) VALUES (?, 1, ?, ?), (?, 1, ?, ?)`,
		"silent-host", now.Add(24*time.Hour).UnixNano(), now.Add(-30*time.Minute).UnixNano(),
		"busy-host", now.Add(-30*time.Minute).UnixNano(), now.Add(-30*time.Minute).UnixNano())
	require.NoError(t, err)
	require.NoError(t, store.UpdateHostLastSeen(ctx, "busy-host", now.Add(-time.Minute)))

	contained := []api.ContainedHost{
		{HostID: "silent-host", Epoch: 7, ContainedAtNs: now.Add(-time.Hour).UnixNano()},
		{HostID: "busy-host", Epoch: 8, ContainedAtNs: now.Add(-time.Hour).UnixNano()},
	}
	insert := func(ctx context.Context, a api.Alert) (bool, error) {
		_, created, err := store.InsertAlert(ctx, a, nil)
		return created, err
	}
	check := containedsilence.New(func(context.Context) ([]api.ContainedHost, error) { return contained, nil },
		store, insert, 0, nil, nil)

	for range 2 {
		_, err := check.Run(ctx)
		require.NoError(t, err)
	}
	alerts := silenceAlerts(ctx, t, store)
	require.Len(t, alerts, 1, "the silent host alerts once however often the check runs, and the reporting host not at all")
	assert.Equal(t, "silent-host", alerts[0].HostID)
	assert.Equal(t, api.SeverityHigh, alerts[0].Severity)
	assert.Equal(t, api.AlertStatusOpen, alerts[0].Status)
	assert.Zero(t, alerts[0].ProcessID, "the alert is about the host, not a process")

	// Alert retention deletes the alert while the host is still silent: the host's own record keeps it from alerting again.
	_, err = db.ExecContext(ctx, `DELETE FROM alerts WHERE rule_id = ?`, containedsilence.RuleID)
	require.NoError(t, err)
	_, err = check.Run(ctx)
	require.NoError(t, err)
	assert.Empty(t, silenceAlerts(ctx, t, store), "the same containment does not alert again after its alert is pruned")

	contained[0].Epoch = 9
	_, err = check.Run(ctx)
	require.NoError(t, err)
	assert.Len(t, silenceAlerts(ctx, t, store), 1, "released and contained anew, the host alerts for the new containment")
}

// silenceAlerts lists the silent-contained-host alerts the operator's alert list shows.
func silenceAlerts(ctx context.Context, t *testing.T, store interface {
	ListAlerts(ctx context.Context, f api.AlertFilter) ([]api.Alert, error)
}) []api.Alert {
	t.Helper()
	all, err := store.ListAlerts(ctx, api.AlertFilter{Limit: 100})
	require.NoError(t, err)
	var out []api.Alert
	for _, a := range all {
		if a.RuleID == containedsilence.RuleID {
			out = append(out, a)
		}
	}
	return out
}
