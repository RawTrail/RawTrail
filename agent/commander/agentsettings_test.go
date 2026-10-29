package commander

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rawtrail/rawtrail/agent/agentsettings"
)

// fakeAgentSettings applies set_agent_settings with a fixed outcome and records what it was given.
type fakeAgentSettings struct {
	applied bool
	err     error
	capture bool
	seen    []agentsettings.Payload
}

func (f *fakeAgentSettings) Apply(p agentsettings.Payload) (bool, error) {
	f.seen = append(f.seen, p)
	return f.applied, f.err
}

func (f *fakeAgentSettings) CapturePrompts() bool { return f.capture }

// spec:agent-command-executor/set-agent-settings-command/the-agent-reports-the-settings-in-force
func TestExecuteSetAgentSettings(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		settings   AgentSettings
		payload    string
		wantStatus string
		wantResult string
	}{
		{"not configured", nil, `{"version":1,"epoch":1,"capture_prompts":false}`, StatusFailed,
			`{"error":"agent settings not configured"}`},
		{"applied", &fakeAgentSettings{applied: true}, `{"version":2,"epoch":9,"capture_prompts":false}`, StatusCompleted,
			`{"version":2,"applied":true,"capture_prompts":false}`},
		{"older than the settings in force", &fakeAgentSettings{capture: true}, `{"version":1,"epoch":3,"capture_prompts":false}`,
			StatusCompleted, `{"version":1,"applied":false,"capture_prompts":true}`},
		{"not recorded", &fakeAgentSettings{applied: true, err: errors.New("write agent settings: read-only file system")},
			`{"version":2,"epoch":9,"capture_prompts":false}`, StatusFailed, `{"error":"write agent settings: read-only file system"}`},
		{"not a payload", &fakeAgentSettings{}, `[]`, StatusFailed,
			`{"error":"invalid payload: json: cannot unmarshal array into Go value of type agentsettings.Payload"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var status string
			var result json.RawMessage
			report := func(_ context.Context, s string, r json.RawMessage) error {
				status, result = s, r
				return nil
			}
			e := NewExecutor(nil, nil, nil)
			e.SetAgentSettings(tc.settings)
			e.Execute(t.Context(), Command{ID: 1, CommandType: "set_agent_settings", Payload: json.RawMessage(tc.payload)}, report)
			assert.Equal(t, tc.wantStatus, status)
			assert.JSONEq(t, tc.wantResult, string(result))
		})
	}
}

// The poll transport hands its configured settings to the executor, decoded.
func TestCommanderWiresAgentSettings(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer srv.Close()
	fake := &fakeAgentSettings{applied: true}
	c := New(Config{ServerURL: srv.URL, HostID: "host-a", AgentSettings: fake}, nil, nil)
	c.dispatch(t.Context(), Command{ID: 13, CommandType: "set_agent_settings", Payload: json.RawMessage(`{"version":4,"epoch":7}`)})
	assert.Equal(t, []agentsettings.Payload{{Version: 4, Epoch: 7}}, fake.seen)
}
