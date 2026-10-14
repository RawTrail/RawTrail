package operator

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fleetdm/edr/server/detection/api"
	visibilityapi "github.com/fleetdm/edr/server/visibility/api"
)

type fakeToolCallReader struct {
	filter    visibilityapi.AgentToolCallFilter
	calls     []visibilityapi.AgentToolCall
	truncated bool
	err       error
}

func (f *fakeToolCallReader) AgentToolCalls(
	_ context.Context, filter visibilityapi.AgentToolCallFilter,
) ([]visibilityapi.AgentToolCall, bool, error) {
	f.filter = filter
	return f.calls, f.truncated, f.err
}

func newToolCallsServer(t *testing.T, lister AgentSessionLister, reader AgentToolCallReader) *httptest.Server {
	t.Helper()
	h := New(fakeService{}, allowAllAuthZ{}, slog.Default())
	if lister != nil {
		h.SetAgentSessions(lister)
	}
	if reader != nil {
		h.SetAgentToolCalls(reader)
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

const toolCallsPath = "/api/hosts/h1/agent-sessions/47abb2a1/tool-calls"

// spec:server-rest-api/agent-tool-calls-endpoint/a-session-s-tool-calls-are-listed-once-each
func TestAgentToolCalls_ListsTheSessionsCalls(t *testing.T) {
	t.Parallel()
	session := api.AgentSessionSummary{HostID: "h1", AgentSessionID: "47abb2a1", FirstSeenNs: 5 * int64(time.Hour), LastSeenNs: 6 * int64(time.Hour)}
	lister := &fakeSessionLister{result: api.AgentSessionResult{Rows: []api.AgentSessionSummary{session}}}
	reader := &fakeToolCallReader{
		calls:     []visibilityapi.AgentToolCall{{ToolUseID: "toolu_a", ToolName: "Bash", Command: "ls", CalledAtNs: 5 * int64(time.Hour)}},
		truncated: true,
	}
	resp := doGet(t, newToolCallsServer(t, lister, reader), toolCallsPath)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out agentToolCallsResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, reader.calls, out.ToolCalls)
	assert.True(t, out.Truncated)
	assert.Equal(t, api.AgentSessionFilter{HostID: "h1", AgentSessionID: "47abb2a1"}, lister.filter)
	// The session's span, widened by an hour on each side for a call made before its process and for the user's approval.
	assert.Equal(t, visibilityapi.AgentToolCallFilter{
		HostID: "h1", SessionID: "47abb2a1", FromNs: 4 * int64(time.Hour), ToNs: 7 * int64(time.Hour), Limit: maxToolCalls,
	}, reader.filter)
}

func TestAgentToolCalls_Errors(t *testing.T) {
	t.Parallel()
	known := &fakeSessionLister{result: api.AgentSessionResult{Rows: []api.AgentSessionSummary{{HostID: "h1", AgentSessionID: "47abb2a1"}}}}
	cases := []struct {
		name   string
		lister AgentSessionLister
		reader AgentToolCallReader
		want   int
	}{
		{name: "unwired", want: http.StatusServiceUnavailable},
		{name: "an unknown session", lister: &fakeSessionLister{}, reader: &fakeToolCallReader{}, want: http.StatusNotFound},
		{name: "the session read fails", lister: &fakeSessionLister{err: errors.New("db")}, reader: &fakeToolCallReader{},
			want: http.StatusInternalServerError},
		{name: "the archive read fails", lister: known, reader: &fakeToolCallReader{err: errors.New("ch")},
			want: http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp := doGet(t, newToolCallsServer(t, tc.lister, tc.reader), toolCallsPath)
			defer resp.Body.Close()
			assert.Equal(t, tc.want, resp.StatusCode)
		})
	}

	t.Run("no calls is an empty list", func(t *testing.T) {
		t.Parallel()
		resp := doGet(t, newToolCallsServer(t, known, &fakeToolCallReader{}), toolCallsPath)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var raw map[string]json.RawMessage
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&raw))
		assert.JSONEq(t, `[]`, string(raw["tool_calls"]))
	})
}
