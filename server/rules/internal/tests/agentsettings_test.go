//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/catchup"
	identityapi "github.com/rawtrail/rawtrail/server/identity/api"
	rulesapi "github.com/rawtrail/rawtrail/server/rules/api"
	rulesbootstrap "github.com/rawtrail/rawtrail/server/rules/bootstrap"
	"github.com/rawtrail/rawtrail/server/rules/internal/agentsettings"
)

const agentSettingsRoute = "/api/v1/detection-config/agent-settings"

type replaceAgentSettingsBody struct {
	Settings     rulesapi.AgentSettings `json:"settings"`
	FanoutHosts  int                    `json:"fanout_hosts"`
	FanoutFailed int                    `json:"fanout_failed"`
}

// typedCommandHistory is fakeCommandHistory keyed by command type as well as host, for a test where the watched-path catch-up runs
// beside this one and queues its own command for the same host.
type typedCommandHistory struct {
	mu     sync.Mutex
	latest map[string]map[string]rulesapi.HostCommand
}

func newTypedCommandHistory() *typedCommandHistory {
	return &typedCommandHistory{latest: map[string]map[string]rulesapi.HostCommand{}}
}

func (f *typedCommandHistory) insert(_ context.Context, hostIDs []string, commandType string, payload []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.latest[commandType] == nil {
		f.latest[commandType] = map[string]rulesapi.HostCommand{}
	}
	for _, h := range hostIDs {
		f.latest[commandType][h] = rulesapi.HostCommand{Payload: payload, Status: catchup.StatusPending, CreatedAt: time.Now()}
	}
	return len(hostIDs), nil
}

func (f *typedCommandHistory) list(_ context.Context, commandType string, hostIDs []string) (map[string]rulesapi.HostCommand, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]rulesapi.HostCommand{}
	for _, h := range hostIDs {
		if c, ok := f.latest[commandType][h]; ok {
			out[h] = c
		}
	}
	return out, nil
}

func (f *typedCommandHistory) get(commandType, hostID string) (rulesapi.HostCommand, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.latest[commandType][hostID]
	return c, ok
}

func (r *appControlRig) agentSettings(t *testing.T) rulesapi.AgentSettings {
	t.Helper()
	resp := r.do(t, http.MethodGet, agentSettingsRoute, nil)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body rulesapi.AgentSettings
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	return body
}

func (r *appControlRig) putAgentSettings(t *testing.T, body map[string]any) *http.Response {
	t.Helper()
	return r.do(t, http.MethodPut, agentSettingsRoute, body)
}

// spec:server-admin-surface/agent-settings-are-configured-over-the-api/prompt-capture-is-on-until-an-administrator-turns-it-off
func TestAgentSettingsREST_CapturesPromptsUntilChanged(t *testing.T) {
	t.Parallel()
	r := newAppControlRig(t, []string{"host-a"})
	got := r.agentSettings(t)
	assert.Equal(t, rulesapi.AgentSettings{Version: 0, CapturePrompts: true}, got)
}

// spec:server-admin-surface/agent-settings-are-configured-over-the-api/turning-prompt-capture-off-is-stored-pushed-and-audited
func TestAgentSettingsREST_TurnsPromptCaptureOffPushesItAndAuditsIt(t *testing.T) {
	t.Parallel()
	hosts := []string{"host-c", "host-a", "host-b"}
	r := newAppControlRig(t, hosts)

	resp := r.putAgentSettings(t, map[string]any{"capture_prompts": false, "reason": "works council", "expected_version": 0})
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var result replaceAgentSettingsBody
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, int64(1), result.Settings.Version)
	assert.False(t, result.Settings.CapturePrompts)
	assert.Equal(t, 3, result.FanoutHosts)
	assert.Zero(t, result.FanoutFailed)

	stored := r.agentSettings(t)
	assert.Equal(t, int64(1), stored.Version)
	assert.False(t, stored.CapturePrompts)
	require.NotNil(t, stored.UpdatedAt)
	assert.Equal(t, r.actor.Principal.ID, stored.UpdatedBy)

	// Written out rather than built from the payload type, so the wire shape the agent decodes is what this pins.
	wantPayload := fmt.Sprintf(`{"version":1,"epoch":%d,"capture_prompts":false}`, stored.UpdatedAt.UnixMicro())
	commands := r.inserter.snapshot()
	require.Len(t, commands, 3)
	gotHosts := make([]string, 0, 3)
	for _, c := range commands {
		assert.Equal(t, rulesapi.CommandTypeSetAgentSettings, c.Type)
		assert.JSONEq(t, wantPayload, string(c.Payload))
		gotHosts = append(gotHosts, c.HostID)
	}
	assert.ElementsMatch(t, hosts, gotHosts)

	e := r.auditRows(t, 1)[0]
	assert.Equal(t, identityapi.AuditDetectionConfigAgentSettingsUpdate, e.Action)
	assert.Equal(t, "agent_settings", e.TargetType)
	assert.Equal(t, "1", e.TargetID)
	assert.Equal(t, r.actor.Principal, e.Actor)
	assert.Equal(t, "works council", e.Payload["reason"])
	assert.Equal(t, false, e.Payload["capture_prompts"])
	assert.Equal(t, true, e.Payload["previous_capture_prompts"])
	assert.EqualValues(t, 0, e.Payload["previous_version"])
	assert.EqualValues(t, 3, e.Payload["fanout_hosts"])
	assert.EqualValues(t, 0, e.Payload["fanout_failed"])

	// Turning it back on is a change like any other: the next version, pushed again.
	back := r.putAgentSettings(t, map[string]any{"capture_prompts": true, "reason": "policy reviewed"})
	back.Body.Close()
	require.Equal(t, http.StatusOK, back.StatusCode)
	assert.Equal(t, rulesapi.AgentSettings{Version: 2, CapturePrompts: true}, withoutAuthor(r.agentSettings(t)))
	assert.Len(t, r.inserter.snapshot(), 6)
}

// withoutAuthor drops who changed the settings and when, which a test compares separately.
func withoutAuthor(s rulesapi.AgentSettings) rulesapi.AgentSettings {
	s.UpdatedAt, s.UpdatedBy, s.UpdatedByLabel = nil, "", ""
	return s
}

// spec:server-admin-surface/agent-settings-are-configured-over-the-api/a-change-without-a-value-or-a-reason-is-refused
func TestAgentSettingsREST_RefusesAChangeWithoutAValueOrAReason(t *testing.T) {
	t.Parallel()
	r := newAppControlRig(t, []string{"host-a"})
	cases := []struct {
		name string
		body map[string]any
	}{
		// A misspelled field must not read as false and turn capture off for the fleet.
		{"no capture_prompts", map[string]any{"capturePrompts": false, "reason": "r"}},
		{"blank reason", map[string]any{"capture_prompts": false, "reason": "  "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := r.putAgentSettings(t, tc.body)
			resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})
	}
	assert.Equal(t, rulesapi.AgentSettings{Version: 0, CapturePrompts: true}, r.agentSettings(t))
	assert.Empty(t, r.inserter.snapshot())
}

// spec:server-admin-surface/agent-settings-are-configured-over-the-api/a-change-to-settings-that-moved-on-is-refused
func TestAgentSettingsREST_RefusesAChangeToSettingsThatMovedOn(t *testing.T) {
	t.Parallel()
	r := newAppControlRig(t, []string{"host-a"})
	for _, capture := range []bool{false, true} {
		resp := r.putAgentSettings(t, map[string]any{"capture_prompts": capture, "reason": "r"})
		resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
	}

	stale := r.putAgentSettings(t, map[string]any{"capture_prompts": false, "reason": "r", "expected_version": 1})
	stale.Body.Close()
	assert.Equal(t, http.StatusConflict, stale.StatusCode)
	assert.Equal(t, rulesapi.AgentSettings{Version: 2, CapturePrompts: true}, withoutAuthor(r.agentSettings(t)))
	assert.Len(t, r.inserter.snapshot(), 2, "the refused change queued nothing")
}

// spec:server-admin-surface/hosts-that-miss-the-agent-settings-push-get-them/a-host-enrolled-after-a-change-is-sent-the-settings
// spec:server-admin-surface/hosts-that-miss-the-agent-settings-push-get-them/unchanged-settings-are-not-sent
func TestAgentSettingsConverge_QueuesTheSettingsForHostsThatMissedThem(t *testing.T) {
	t.Parallel()
	r := newAppControlRig(t, []string{"host-a"})
	history := &fakeCommandHistory{latest: map[string]rulesapi.HostCommand{}}
	enrolled := []rulesapi.HostEnrollment{
		{HostID: "host-a", EnrolledAt: time.Now().Add(-48 * time.Hour)},
		{HostID: "late-host", EnrolledAt: time.Now().Add(-time.Hour)},
	}
	converger := agentsettings.NewConverger(agentsettings.NewStore(r.db), history.insert,
		func(context.Context) ([]rulesapi.HostEnrollment, error) { return enrolled, nil }, history.list, slog.Default())

	queued, err := converger.Converge(t.Context())
	require.NoError(t, err)
	assert.Zero(t, queued, "nothing is sent while the settings have never been changed")

	put := r.putAgentSettings(t, map[string]any{"capture_prompts": false, "reason": "r"})
	put.Body.Close()
	require.Equal(t, http.StatusOK, put.StatusCode)
	for _, c := range r.inserter.snapshot() {
		history.latest[c.HostID] = rulesapi.HostCommand{Payload: c.Payload, Status: catchup.StatusCompleted, CreatedAt: time.Now()}
	}

	queued, err = converger.Converge(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, queued)
	assert.Equal(t, []string{"late-host"}, history.takeQueued())
	var sent rulesapi.SetAgentSettingsPayload
	require.NoError(t, json.Unmarshal(history.latest["late-host"].Payload, &sent))
	stored := r.agentSettings(t)
	assert.Equal(t, rulesapi.SetAgentSettingsPayload{Version: stored.Version, Epoch: stored.UpdatedAt.UnixMicro(), CapturePrompts: false},
		sent, "a caught-up host gets the same settings, epoch included, as the push")

	queued, err = converger.Converge(t.Context())
	require.NoError(t, err)
	assert.Zero(t, queued, "a host whose copy is pending is not sent another")
}

// The catch-up runs as one of the rules context's loops, wired from the same dependencies as the watched-path one.
func TestAgentSettingsConverge_RunsAsARulesContextLoop(t *testing.T) {
	t.Parallel()
	history := newTypedCommandHistory()
	r := newAppControlRig(t, []string{"host-a"}, func(d *rulesbootstrap.Deps) {
		d.CommandBatchInserter = history.insert
		d.HostEnrollments = func(context.Context) ([]rulesapi.HostEnrollment, error) {
			return []rulesapi.HostEnrollment{{HostID: "late-host", EnrolledAt: time.Now().Add(-time.Hour)}}, nil
		}
		d.HostLatestCommands = history.list
		d.WatchedPathConvergeInterval = 20 * time.Millisecond
	})
	put := r.putAgentSettings(t, map[string]any{"capture_prompts": false, "reason": "r"})
	put.Body.Close()
	require.Equal(t, http.StatusOK, put.StatusCode)
	_, before := history.get(rulesapi.CommandTypeSetAgentSettings, "late-host")
	require.False(t, before, "the push went to the rig's enrolled host only")

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { r.rules.Run(ctx); close(done) }()
	require.Eventually(t, func() bool {
		c, ok := history.get(rulesapi.CommandTypeSetAgentSettings, "late-host")
		var sent rulesapi.SetAgentSettingsPayload
		return ok && json.Unmarshal(c.Payload, &sent) == nil && sent.Version == 1 && !sent.CapturePrompts
	}, 5*time.Second, 20*time.Millisecond)
	cancel()
	<-done
}
