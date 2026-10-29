package operator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	identityapi "github.com/rawtrail/rawtrail/server/identity/api"
	"github.com/rawtrail/rawtrail/server/rules/api"
	"github.com/rawtrail/rawtrail/server/rules/internal/agentsettings"
	"github.com/rawtrail/rawtrail/server/rules/internal/hostpush"
)

type fakeAgentSettings struct {
	settings   api.AgentSettings
	getErr     error
	replaceErr error
	gotCapture *bool
	gotExpect  *int64
}

func (f *fakeAgentSettings) Get(context.Context) (api.AgentSettings, error) {
	return f.settings, f.getErr
}

func (f *fakeAgentSettings) Replace(_ context.Context, actor *identityapi.Actor, _ string, capturePrompts bool,
	expectedVersion *int64) (agentsettings.ReplaceResult, error) {
	f.gotCapture, f.gotExpect = &capturePrompts, expectedVersion
	if f.replaceErr != nil {
		return agentsettings.ReplaceResult{}, f.replaceErr
	}
	settings := api.AgentSettings{Version: 2, CapturePrompts: capturePrompts, UpdatedBy: actor.Principal.ID}
	return agentsettings.ReplaceResult{Settings: settings, Counts: hostpush.Counts{FanoutHosts: 2}}, nil
}

var _ agentSettingsService = (*agentsettings.Service)(nil)

func agentSettingsServer(t *testing.T, svc agentSettingsService) *httptest.Server {
	t.Helper()
	h := NewDetectionConfig(&fakeDCService{}, allowAllAuthZ{}, slog.Default())
	h.SetAgentSettings(svc)
	h.SetPrincipalLabelResolver(func(context.Context, string) (string, error) { return "ops@example.com", nil })
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := identityapi.WithActor(r.Context(), &identityapi.Actor{Principal: identityapi.UserPrincipal(7, "")})
		mux.ServeHTTP(w, r.WithContext(ctx))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAgentSettingsHandler_Get(t *testing.T) {
	t.Parallel()
	t.Run("names who last changed them", func(t *testing.T) {
		t.Parallel()
		svc := &fakeAgentSettings{settings: api.AgentSettings{Version: 3, CapturePrompts: false, UpdatedBy: "usr_7"}}
		resp := dcDo(t, agentSettingsServer(t, svc), http.MethodGet, "/api/v1/detection-config/agent-settings", "")
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var body api.AgentSettings
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.Equal(t, api.AgentSettings{Version: 3, UpdatedBy: "usr_7", UpdatedByLabel: "ops@example.com"}, body)
	})
	t.Run("a read failure is a server error that does not leak its cause", func(t *testing.T) {
		t.Parallel()
		svc := &fakeAgentSettings{getErr: errors.New("database unavailable")}
		resp := dcDo(t, agentSettingsServer(t, svc), http.MethodGet, "/api/v1/detection-config/agent-settings", "")
		defer resp.Body.Close()
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		raw := new(bytes.Buffer)
		_, _ = raw.ReadFrom(resp.Body)
		assert.NotContains(t, raw.String(), "database unavailable")
	})
}

func TestAgentSettingsHandler_ReplaceMapsEachOutcome(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		body       string
		err        error
		wantStatus int
		wantInBody string
	}{
		{"stored", `{"capture_prompts":false,"reason":"r","expected_version":1}`, nil, http.StatusOK, `"fanout_hosts":2`},
		{"no reason", `{"capture_prompts":false,"reason":""}`, agentsettings.ErrReasonRequired, http.StatusBadRequest, "reason is required"},
		{"changed since read", `{"capture_prompts":false,"reason":"r"}`, agentsettings.ErrVersionConflict, http.StatusConflict,
			"changed since they were read"},
		{"store failure", `{"capture_prompts":false,"reason":"r"}`, errors.New("database unavailable"), http.StatusInternalServerError,
			"internal error"},
		{"not JSON", `{`, nil, http.StatusBadRequest, "detection_config.invalid_json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := &fakeAgentSettings{replaceErr: tc.err}
			resp := dcDo(t, agentSettingsServer(t, svc), http.MethodPut, "/api/v1/detection-config/agent-settings", tc.body)
			defer resp.Body.Close()
			assert.Equal(t, tc.wantStatus, resp.StatusCode)
			raw := new(bytes.Buffer)
			_, _ = raw.ReadFrom(resp.Body)
			assert.Contains(t, raw.String(), tc.wantInBody)
			assert.NotContains(t, raw.String(), "database unavailable", "a server error must not leak its cause")
		})
	}

	t.Run("passes the value and the expected version through", func(t *testing.T) {
		t.Parallel()
		svc := &fakeAgentSettings{}
		resp := dcDo(t, agentSettingsServer(t, svc), http.MethodPut, "/api/v1/detection-config/agent-settings",
			`{"capture_prompts":false,"reason":"r","expected_version":1}`)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.NotNil(t, svc.gotCapture)
		assert.False(t, *svc.gotCapture)
		require.NotNil(t, svc.gotExpect)
		assert.Equal(t, int64(1), *svc.gotExpect)
		var body agentsettings.ReplaceResult
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.Equal(t, "ops@example.com", body.Settings.UpdatedByLabel)
	})
}

// The PUT body round-trips with capture_prompts and expected_version each absent or present, since absence is what the handler refuses
// and what makes the change unconditional.
func TestReplaceAgentSettingsRequest_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		want := replaceAgentSettingsRequest{Reason: rapid.String().Draw(t, "reason")}
		if rapid.Bool().Draw(t, "has_capture_prompts") {
			capture := rapid.Bool().Draw(t, "capture_prompts")
			want.CapturePrompts = &capture
		}
		if rapid.Bool().Draw(t, "has_expected_version") {
			version := rapid.Int64().Draw(t, "expected_version")
			want.ExpectedVersion = &version
		}
		b, err := json.Marshal(want)
		require.NoError(t, err)
		var got replaceAgentSettingsRequest
		require.NoError(t, json.Unmarshal(b, &got))
		assert.Equal(t, want, got)
	})
}
