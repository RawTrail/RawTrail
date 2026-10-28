package operator

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

// A scoped tree read pages by cursor (issue #1424): a cut page names where the next starts, the next page reads from there, and a
// session's agents lead its first page only.
//
// spec:server-rest-api/agent-session-tree-pages/a-session-tree-pages-by-cursor
func TestProcessTreeHandler_ScopedReadPagesByCursor(t *testing.T) {
	t.Parallel()
	var asked []api.TreeScope
	svc := fakeService{buildScopedTree: func(_ context.Context, _ string, scope api.TreeScope, _ int, _ bool, _ int64) (api.ProcessTreeResult, error) {
		asked = append(asked, scope)
		if scope.After == (api.TreePosition{}) {
			return api.ProcessTreeResult{Roots: []api.ProcessNode{{Process: api.Process{ID: 7, ForkTimeNs: 700}}}, Truncated: true,
				Next: &api.TreePosition{ForkTimeNs: 700, ID: 7}}, nil
		}
		return api.ProcessTreeResult{Roots: []api.ProcessNode{{Process: api.Process{ID: 8, ForkTimeNs: 800}}}}, nil
	}}
	lister := &fakeSessionLister{}
	h := New(svc, allowAllAuthZ{}, slog.Default())
	h.SetAgentSessions(lister)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	read := func(query string) (int, api.ProcessTreeResult) {
		resp := doGet(t, srv, "/api/hosts/h1/tree?scope=session&session=s1&flatten=1"+query)
		defer resp.Body.Close()
		var out api.ProcessTreeResult
		if resp.StatusCode == http.StatusOK {
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		}
		return resp.StatusCode, out
	}

	status, first := read("")
	require.Equal(t, http.StatusOK, status)
	require.NotEmpty(t, first.NextCursor)
	assert.Equal(t, api.AgentSessionFilter{HostID: "h1", AgentSessionID: "s1"}, lister.filter, "the first page reads the agents")

	lister.filter = api.AgentSessionFilter{}
	status, second := read("&cursor=" + first.NextCursor)
	require.Equal(t, http.StatusOK, status)
	assert.Empty(t, second.NextCursor, "the last page names no next")
	require.Len(t, second.Roots, 1)
	assert.Equal(t, int64(8), second.Roots[0].ID)
	require.Len(t, asked, 2)
	assert.Equal(t, api.TreePosition{ForkTimeNs: 700, ID: 7}, asked[1].After, "the next page reads after the first's last row")
	assert.Equal(t, api.AgentSessionFilter{}, lister.filter, "a later page does not read the agents again")

	for _, bad := range []string{"!!", "bm9wZQ", "e30", "eyJmIjoxfQ"} { // not base64url, not JSON, {}, {"f":1}
		status, _ := read("&cursor=" + bad)
		assert.Equal(t, http.StatusBadRequest, status, bad)
	}
	assert.Len(t, asked, 2, "a bad cursor reads nothing")
}
