package operator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

const processesPath = "/api/hosts/h1/agent-sessions/47abb2a1/processes"

type processesPage struct {
	Processes []struct {
		ID       int64             `json:"id"`
		Path     string            `json:"path"`
		Children []json.RawMessage `json:"children"`
	} `json:"processes"`
	NextCursor   string `json:"next_cursor"`
	TotalMatched int    `json:"total_matched"`
	Truncated    bool   `json:"truncated"`
}

func getProcesses(t *testing.T, srv *httptest.Server, query string) (int, processesPage) {
	t.Helper()
	resp := doGet(t, srv, processesPath+query)
	defer resp.Body.Close()
	var out processesPage
	if resp.StatusCode == http.StatusOK {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	}
	return resp.StatusCode, out
}

// spec:server-rest-api/agent-session-process-search/a-search-finds-a-process-on-any-page-of-the-session
// spec:server-rest-api/agent-session-process-search/a-search-pages-by-cursor
func TestAgentSessionProcesses_SearchesAndPages(t *testing.T) {
	t.Parallel()
	srv := newCommandsServer(t, fakeService{buildScopedTree: commandsTree(true)}, commandsCalls(), summarySession())

	// The three wrapper shells match by path; their children do not.
	status, first := getProcesses(t, srv, "?q=%2FBIN%2FZSH&limit=2")
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, []int64{1, 3}, []int64{first.Processes[0].ID, first.Processes[1].ID})
	assert.Equal(t, 3, first.TotalMatched)
	assert.True(t, first.Truncated, "the session read was cut, so a match past it is not found")
	for _, p := range first.Processes {
		assert.Empty(t, p.Children, "each match is listed alone")
	}
	require.NotEmpty(t, first.NextCursor)

	status, second := getProcesses(t, srv, "?q=%2Fbin%2Fzsh&limit=2&cursor="+first.NextCursor)
	require.Equal(t, http.StatusOK, status)
	require.Len(t, second.Processes, 1)
	assert.Equal(t, int64(6), second.Processes[0].ID)
	assert.Empty(t, second.NextCursor)

	// TextEdit is a root of its own in the read; a search finds it by its command line, under no command.
	status, edit := getProcesses(t, srv, "?q=textedit")
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, 3, edit.TotalMatched, "the call's shell and open name it, and TextEdit itself")

	status, _ = getProcesses(t, srv, "?q=zsh&cursor=not-a-cursor")
	assert.Equal(t, http.StatusBadRequest, status)
}

func TestAgentSessionProcesses_Failures(t *testing.T) {
	t.Parallel()
	treeFails := func(context.Context, string, api.TreeScope, int, bool, int64) (api.ProcessTreeResult, error) {
		return api.ProcessTreeResult{}, errors.New("db down")
	}
	for _, tc := range []struct {
		name   string
		tree   func(context.Context, string, api.TreeScope, int, bool, int64) (api.ProcessTreeResult, error)
		lister AgentSessionLister
		want   int
	}{
		{"an unknown session", commandsTree(false), &fakeSessionLister{}, http.StatusNotFound},
		{"the process read fails", treeFails, summarySession(), http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newCommandsServer(t, fakeService{buildScopedTree: tc.tree}, commandsCalls(), tc.lister)
			status, _ := getProcesses(t, srv, "?q=zsh")
			assert.Equal(t, tc.want, status)
		})
	}
}
