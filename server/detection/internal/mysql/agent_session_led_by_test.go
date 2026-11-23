package mysql_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

// spec:server-detection-rules-engine/an-ai-agent-reading-credentials-is-reported/the-agent-s-own-read-joins-the-session-it-leads
//
// TestGetAgentSessionLedBy pins which session an agent process leads at a moment (issue #1589): the latest one recording its pid
// as the agent's, first seen during the process's own lifetime and at or before the moment.
func TestGetAgentSessionLedBy(t *testing.T) {
	t.Parallel()
	store, db := newStoreAndDB(t)
	ctx := t.Context()
	const host = "led-by-host"
	for _, s := range []struct {
		id        string
		pid       int
		firstSeen int64
	}{
		{"recycled-pid-session", 6833, 100},  // an earlier process that had the same pid
		{"earlier-image-session", 6833, 950}, // the same pid's previous image: a re-exec chain shares one fork time
		{"first-session", 6833, 1_100},
		{"cleared-session", 6833, 1_500}, // /clear starts a new session under the same agent process
		{"another-agent-session", 7000, 1_200},
	} {
		_, err := db.ExecContext(ctx, `INSERT INTO agent_sessions (host_id, agent_session_id, agent_name, agent_pid, first_seen_ns,
			first_event_id, last_seen_ns) VALUES (?, ?, 'claude-code', ?, ?, ?, ?)`, host, s.id, s.pid, s.firstSeen, s.id, s.firstSeen)
		require.NoError(t, err)
	}
	execNs := int64(1_000)
	agent := api.Process{HostID: host, PID: 6833, ForkTimeNs: 900, ExecTimeNs: &execNs}

	cases := []struct {
		name  string
		agent api.Process
		atNs  int64
		want  string
	}{
		{"the session first seen before the read", agent, 1_200, "first-session"},
		{"the newest session once /clear started another", agent, 1_600, "cleared-session"},
		{"a session first seen at the read itself", agent, 1_500, "cleared-session"},
		{"none before the process's first session, nor its previous image's", agent, 1_050, ""},
		{"never a recycled pid's earlier session", api.Process{HostID: host, PID: 6833, ForkTimeNs: 1_000}, 1_050, ""},
		{"from the fork when the process never execed", api.Process{HostID: host, PID: 6833, ForkTimeNs: 50}, 1_050, "earlier-image-session"},
		{"a pid that leads nothing", api.Process{HostID: host, PID: 7400, ForkTimeNs: 0}, 2_000, ""},
		{"another host's sessions", api.Process{HostID: "other-host", PID: 6833, ForkTimeNs: 0}, 2_000, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := store.GetAgentSessionLedBy(ctx, tc.agent, tc.atNs)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
