//go:build integration

// An alert that carries its AI agent session (issue #1589): agent_credential_read groups the agent's own read with its session's
// alert, and an alert whose first read was the agent's is anchored on the agent's process, which carries no session marker.

package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

// spec:server-detection-rules-engine/an-ai-agent-reading-credentials-is-reported/an-alert-anchored-on-the-agent-stays-with-its-session
func TestSessionViews_CountAnAlertCarryingItsSession(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	seedSessionRow(t, db, "host-a", "s1", "claude-code", "/Users/v/project", 100, 900)
	inSession := seedSessionProcesses(t, store, "host-a", "s1", 1)[0]
	agentProcess := func(host string) int64 {
		id, err := store.InsertProcess(t.Context(), api.Process{HostID: host, PID: 80673, PPID: 1, Path: "/opt/claude", ForkTimeNs: 50})
		require.NoError(t, err)
		return id
	}
	agent, otherHostAgent := agentProcess("host-a"), agentProcess("host-b")
	raise := func(host string, processID int64, subject, session string) int64 {
		id, _, err := store.InsertAlert(t.Context(), api.Alert{HostID: host, RuleID: "agent_credential_read",
			Source: api.AlertSourceDetection, Severity: api.SeverityHigh, Title: "t", Description: "d", ProcessID: processID,
			Subject: subject, AgentSessionID: session}, nil)
		require.NoError(t, err)
		return id
	}
	want := []int64{
		raise("host-a", agent, "agent-first", "s1"),          // anchored outside the session, carrying it
		raise("host-a", inSession, "command-first", "s1"),    // anchored in the session and carrying it: counted once
		raise("host-a", inSession, "no-session-carried", ""), // the process's session, as every other rule's alert
	}
	raise("host-a", agent, "another-session", "s2")
	raise("host-b", otherHostAgent, "same-session-other-host", "s1")

	alerts, err := store.ListAlerts(t.Context(), api.AlertFilter{HostID: "host-a", AgentSessionID: "s1"})
	require.NoError(t, err)
	var got []int64
	for _, a := range alerts {
		got = append(got, a.ID)
	}
	assert.ElementsMatch(t, want, got, "the session's alerts, whether they carry the session or their process does")

	counts, err := store.AgentSessionCounts(t.Context(), "host-a", "s1")
	require.NoError(t, err)
	assert.Equal(t, map[string]int64{api.SeverityHigh: 3}, counts.AlertsBySeverity, "the session header counts the same three")

	list, err := store.ListAgentSessions(t.Context(), api.AgentSessionFilter{HostID: "host-a"}, "", 10)
	require.NoError(t, err)
	require.Len(t, list.Rows, 1)
	assert.Equal(t, int64(3), list.Rows[0].AlertCount, "and so does the sessions list")
}
