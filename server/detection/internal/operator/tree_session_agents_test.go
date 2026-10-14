package operator

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fleetdm/edr/server/detection/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A session's tree read carries the session's agent process beside its roots (issue #1334): the agent has no session marker, so it is
// not a root, and the Processes view hangs each root under the agent its row names as creator.
//
// spec:server-rest-api/session-tree-names-its-agent/a-session-s-tree-names-the-agent-that-ran-its-commands
func TestProcessTreeHandler_SessionNamesItsAgent(t *testing.T) {
	t.Parallel()
	agentRow, otherCreator, agentPID := int64(50), int64(60), 4911
	agentProc := api.Process{ID: agentRow, HostID: "h1", PID: agentPID, Path: "/opt/claude/2.1.296"}
	root := func(id int64, pid int, creator int64) api.ProcessNode {
		n := activityNode(id, pid, 1)
		n.ParentProcessID = &creator
		return n
	}
	// Two commands the agent ran, and a root another process created, such as a launchd launch.
	roots := []api.ProcessNode{root(1, 5001, agentRow), root(2, 5002, agentRow), root(3, 5003, otherCreator)}
	processes := map[int64]api.Process{agentRow: agentProc, otherCreator: {ID: otherCreator, HostID: "h1", PID: 1, Path: "/sbin/launchd"}}
	sessions := api.AgentSessionResult{Rows: []api.AgentSessionSummary{{HostID: "h1", AgentSessionID: "47abb2a1", AgentPID: &agentPID}}}
	serve := func(lister *fakeSessionLister, query string) (int, api.ProcessTreeResult) {
		svc := fakeService{buildScopedTree: func(context.Context, string, api.TreeScope, int, bool, int64) (api.ProcessTreeResult, error) {
			return api.ProcessTreeResult{Roots: roots, Returned: 3, TotalMatched: 3}, nil
		}}
		h := New(svc, allowAllAuthZ{}, slog.Default())
		h.SetActivity(&fakeActivityReader{processes: processes})
		h.SetAgentSessions(lister)
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)
		resp := doGet(t, srv, "/api/hosts/h1/tree"+query)
		defer resp.Body.Close()
		var out api.ProcessTreeResult
		if resp.StatusCode == http.StatusOK {
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		}
		return resp.StatusCode, out
	}

	t.Run("a session read names its agent and leaves its roots and counts alone", func(t *testing.T) {
		t.Parallel()
		status, out := serve(&fakeSessionLister{result: sessions}, "?scope=session&session=47abb2a1&flatten=1")
		require.Equal(t, http.StatusOK, status)
		require.Len(t, out.Agents, 1)
		assert.Equal(t, agentRow, out.Agents[0].ID)
		assert.Len(t, out.Roots, 3, "the commands stay roots, so the session view's groups are unchanged")
		assert.Equal(t, int64(3), out.Returned)
	})

	t.Run("the agent lookup does not pay for the session list's counts", func(t *testing.T) {
		t.Parallel()
		lister := &fakeSessionLister{result: sessions}
		status, out := serve(lister, "?scope=session&session=47abb2a1&flatten=1")
		require.Equal(t, http.StatusOK, status)
		require.Len(t, out.Agents, 1)
		assert.Zero(t, lister.limit, "the list, which counts each session's processes and alerts, is never read")
	})

	t.Run("a story read names no agent", func(t *testing.T) {
		t.Parallel()
		lister := &fakeSessionLister{result: sessions}
		status, out := serve(lister, "?scope=story&root=1")
		require.Equal(t, http.StatusOK, status)
		assert.Empty(t, out.Agents)
		assert.Empty(t, lister.filter.AgentSessionID, "a story read does not look up a session")
	})

	t.Run("a failed session lookup fails the read", func(t *testing.T) {
		t.Parallel()
		status, _ := serve(&fakeSessionLister{err: errors.New("db down")}, "?scope=session&session=47abb2a1")
		assert.Equal(t, http.StatusInternalServerError, status)
	})
}
