package operator

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
	identityapi "github.com/rawtrail/rawtrail/server/identity/api"
)

type fakeSessionLister struct {
	filter api.AgentSessionFilter
	cursor string
	limit  int
	result api.AgentSessionResult
	err    error
}

func (f *fakeSessionLister) ListAgentSessions(_ context.Context, filter api.AgentSessionFilter, cursor string, limit int) (api.AgentSessionResult, error) {
	f.filter, f.cursor, f.limit = filter, cursor, limit
	return f.result, f.err
}

// GetAgentSession answers from result's rows, recording the lookup in filter as the list does, and never calls the list: a lookup that
// paid for the list's counts would leave limit set.
func (f *fakeSessionLister) GetAgentSession(_ context.Context, hostID, sessionID string) (*api.AgentSessionSummary, error) {
	f.filter = api.AgentSessionFilter{HostID: hostID, AgentSessionID: sessionID}
	if f.err != nil {
		return nil, f.err
	}
	for _, r := range f.result.Rows {
		if r.HostID == hostID && r.AgentSessionID == sessionID {
			return &r, nil
		}
	}
	return nil, nil
}

func newSessionsServer(t *testing.T, lister AgentSessionLister, az identityapi.AuthZ) *httptest.Server {
	t.Helper()
	h := New(fakeService{}, az, slog.Default())
	if lister != nil {
		h.SetAgentSessions(lister)
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// spec:server-rest-api/agent-sessions-list/filters-narrow-the-list
func TestAgentSessionsHandler_PassesFiltersAndPaging(t *testing.T) {
	t.Parallel()
	lister := &fakeSessionLister{result: api.AgentSessionResult{
		Rows:       []api.AgentSessionSummary{{HostID: "h1", AgentSessionID: "s1", AgentName: "claude-code", ProcessCount: 4, AlertCount: 1}},
		NextCursor: "next", TotalMatched: 9,
	}}
	srv := newSessionsServer(t, lister, allowAllAuthZ{})
	resp := doGet(t, srv, "/api/agent-sessions?host_id=h1&session=s1&agent=claude-code&from=100&to=900&cursor=abc&limit=25")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, api.AgentSessionFilter{HostID: "h1", AgentSessionID: "s1", AgentName: "claude-code", FromNs: 100, ToNs: 900},
		lister.filter)
	assert.Equal(t, "abc", lister.cursor)
	assert.Equal(t, 25, lister.limit)
	var body api.AgentSessionResult
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, lister.result, body)
}

func TestAgentSessionsHandler_EmptyPageIsAnArray(t *testing.T) {
	t.Parallel()
	srv := newSessionsServer(t, &fakeSessionLister{}, allowAllAuthZ{})
	resp := doGet(t, srv, "/api/agent-sessions")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var raw map[string]json.RawMessage
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&raw))
	assert.JSONEq(t, `[]`, string(raw["rows"]))
}

// spec:server-rest-api/agent-sessions-list/the-list-pages-by-cursor
func TestAgentSessionsHandler_Errors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		query  string
		err    error
		lister bool
		want   int
	}{
		{name: "malformed cursor", query: "?cursor=zz", err: api.ErrInvalidCursor, lister: true, want: http.StatusBadRequest},
		{name: "inverted window", query: "?from=900&to=100", lister: true, want: http.StatusBadRequest},
		{name: "negative bound", query: "?from=-1", lister: true, want: http.StatusBadRequest},
		{name: "store failure", err: errors.New("boom"), lister: true, want: http.StatusInternalServerError},
		{name: "surface not wired", want: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var lister AgentSessionLister
			if tc.lister {
				lister = &fakeSessionLister{err: tc.err}
			}
			resp := doGet(t, newSessionsServer(t, lister, allowAllAuthZ{}), "/api/agent-sessions"+tc.query)
			defer resp.Body.Close()
			assert.Equal(t, tc.want, resp.StatusCode)
		})
	}
}

// The list reveals processes fleet-wide, so it takes the process-read grant.
func TestAgentSessionsHandler_RequiresProcessRead(t *testing.T) {
	t.Parallel()
	lister := &fakeSessionLister{}
	resp := doGet(t, newSessionsServer(t, lister, denyAllAuthZ{}), "/api/agent-sessions")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Zero(t, lister.limit, "the store is not read without the grant")
}
