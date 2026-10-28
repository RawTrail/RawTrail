//go:build integration

// A session summary's process and alert counts (issue #1422): the session's retained processes and the alerts raised on them by
// severity, as the sessions list counts them.

package tests

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

// spec:server-rest-api/agent-session-summary/a-session-is-totalled
func TestAgentSessionCounts(t *testing.T) {
	t.Parallel()
	store, _ := openProcessStore(t)
	procs := seedSessionProcesses(t, store, "host-a", "s1", 3)
	other := seedSessionProcesses(t, store, "host-a", "s2", 1)
	elsewhere := seedSessionProcesses(t, store, "host-b", "s1", 1)
	alerts := []struct {
		process     int64
		severity    string
		disposition api.AlertDisposition
	}{
		{procs[0], api.SeverityHigh, api.AlertDispositionAlert},
		{procs[1], api.SeverityHigh, api.AlertDispositionAlert},
		{procs[2], api.SeverityCritical, api.AlertDispositionAlert},
		{procs[2], api.SeverityLow, api.AlertDispositionMonitor},
		{other[0], api.SeverityMedium, api.AlertDispositionAlert},
		{elsewhere[0], api.SeverityMedium, api.AlertDispositionAlert},
	}
	for i, a := range alerts {
		host := "host-a"
		if a.process == elsewhere[0] {
			host = "host-b"
		}
		_, _, err := store.InsertAlert(t.Context(), api.Alert{HostID: host, RuleID: "r" + strconv.Itoa(i), Source: api.AlertSourceDetection,
			Severity: a.severity, Title: "t", Description: "d", ProcessID: a.process, Subject: "proc:" + strconv.Itoa(i),
			Disposition: a.disposition}, nil)
		require.NoError(t, err)
	}

	got, err := store.AgentSessionCounts(t.Context(), "host-a", "s1")
	require.NoError(t, err)
	assert.Equal(t, api.AgentSessionCounts{
		Processes:        3,
		AlertsBySeverity: map[string]int64{api.SeverityHigh: 2, api.SeverityCritical: 1},
	}, got, "the monitor record, the other session's alert and the other host's are not counted")

	t.Run("a session with no processes counts nothing", func(t *testing.T) {
		t.Parallel()
		none, err := store.AgentSessionCounts(t.Context(), "host-a", "missing")
		require.NoError(t, err)
		assert.Equal(t, api.AgentSessionCounts{AlertsBySeverity: map[string]int64{}}, none)
	})
}
