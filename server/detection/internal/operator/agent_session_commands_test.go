package operator

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/internal/graph"
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

const commandsPath = "/api/hosts/h1/agent-sessions/47abb2a1/commands"

// commandProc is a process of the commands tests: an exec with its argv, run as uid 501.
func commandProc(id int64, pid int, forkNs int64, path string, argv []string, children ...api.ProcessNode) api.ProcessNode {
	raw, _ := json.Marshal(argv)
	return api.ProcessNode{Process: api.Process{ID: id, PID: pid, Path: path, Args: raw, ForkTimeNs: forkNs, UID: new(501)},
		Children: children}
}

// toolCallShell is the agent's wrapper shell running one Bash call, leading its process group.
func toolCallShell(id int64, pid int, forkNs int64, command string, children ...api.ProcessNode) api.ProcessNode {
	n := commandProc(id, pid, forkNs, "/bin/zsh", []string{"/bin/zsh", "-c", "source /tmp/snap.sh && eval '" + command + "' < /dev/null"},
		children...)
	n.ToolCallGroup = &pid
	return n
}

// commandsTree is the session's process read: three Bash calls, the second of which opened TextEdit, which launchd started as a root
// the inferred-launch link moves under the call's open.
func commandsTree(truncated bool) func(context.Context, string, api.TreeScope, int, bool, int64) (api.ProcessTreeResult, error) {
	return func(_ context.Context, _ string, scope api.TreeScope, limit int, flatten bool, _ int64) (api.ProcessTreeResult, error) {
		if scope.AgentSessionID != "47abb2a1" || limit != graph.MaxScopedRead || !flatten {
			return api.ProcessTreeResult{}, errors.New("unexpected session read")
		}
		return api.ProcessTreeResult{Roots: []api.ProcessNode{
			toolCallShell(1, 100, 1000, "ls", commandProc(2, 101, 1001, "/bin/ls", []string{"ls"})),
			toolCallShell(3, 200, 2000, "open -a TextEdit", commandProc(4, 201, 2001, "/usr/bin/open", []string{"open", "-a", "TextEdit"})),
			commandProc(5, 300, 2002, "/System/Applications/TextEdit.app/Contents/MacOS/TextEdit", []string{"TextEdit"}),
			toolCallShell(6, 400, 3000, "git status", commandProc(7, 401, 3001, "/usr/bin/git", []string{"git", "status"})),
		}, Links: []api.ProcessLink{{From: 4, To: 5, Kind: api.ProcessLinkInferredLaunch}}, Truncated: truncated}, nil
	}
}

func commandsCalls() *fakeToolCallReader {
	return &fakeToolCallReader{calls: []visibilityapi.AgentToolCall{
		{ToolUseID: "toolu_ls", ToolName: "Bash", Command: "ls", CalledAtNs: 900, UID: 501},
		{ToolUseID: "toolu_git", ToolName: "Bash", Command: "git status", CalledAtNs: 2900, UID: 501},
	}}
}

func newCommandsServer(t *testing.T, svc fakeService, reader *fakeToolCallReader, lister AgentSessionLister) *httptest.Server {
	t.Helper()
	h := New(svc, allowAllAuthZ{}, slog.Default())
	h.SetAgentSessions(lister)
	h.SetAgentToolCalls(reader)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// commandsPage is the response as a client reads it.
type commandsPage struct {
	Commands []struct {
		Key          string  `json:"key"`
		Command      string  `json:"command"`
		FromAgent    bool    `json:"from_agent"`
		ProcessIDs   []int64 `json:"process_ids"`
		ProcessesCut bool    `json:"processes_cut"`
		Roots        []struct {
			ID       int64 `json:"id"`
			Children []struct {
				ID       int64 `json:"id"`
				Children []struct {
					ID int64 `json:"id"`
				} `json:"children"`
			} `json:"children"`
		} `json:"roots"`
		ToolCall *struct {
			Kind string `json:"kind"`
			Call struct {
				ToolUseID string `json:"tool_use_id"`
			} `json:"call"`
		} `json:"tool_call"`
	} `json:"commands"`
	NextCursor         string  `json:"next_cursor"`
	TotalMatched       int     `json:"total_matched"`
	Inferred           []int64 `json:"inferred"`
	Services           []int64 `json:"services"`
	Truncated          bool    `json:"truncated"`
	ToolCallsTruncated bool    `json:"tool_calls_truncated"`
}

func getCommands(t *testing.T, srv *httptest.Server, query string) (int, commandsPage) {
	t.Helper()
	resp := doGet(t, srv, commandsPath+query)
	defer resp.Body.Close()
	var out commandsPage
	if resp.StatusCode == http.StatusOK {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	}
	return resp.StatusCode, out
}

// spec:server-rest-api/agent-session-commands/commands-are-labelled-and-paired-with-their-tool-calls
// spec:server-rest-api/agent-session-commands/what-launchd-started-is-listed-under-the-command-that-asked-for-it
func TestAgentSessionCommands_Lists(t *testing.T) {
	t.Parallel()
	reader := commandsCalls()
	srv := newCommandsServer(t, fakeService{buildScopedTree: commandsTree(false)}, reader, summarySession())

	status, got := getCommands(t, srv, "")
	require.Equal(t, http.StatusOK, status)
	require.Len(t, got.Commands, 3)
	assert.Equal(t, 3, got.TotalMatched)
	assert.Empty(t, got.NextCursor)
	ls, open, git := got.Commands[0], got.Commands[1], got.Commands[2]
	assert.Equal(t, []string{"ls", "open -a TextEdit", "git status"}, []string{ls.Command, open.Command, git.Command})
	assert.Equal(t, "row:1", ls.Key)
	assert.True(t, ls.FromAgent)

	require.NotNil(t, ls.ToolCall)
	assert.Equal(t, "toolu_ls", ls.ToolCall.Call.ToolUseID)
	assert.Nil(t, open.ToolCall, "the agent recorded no call for it")
	require.NotNil(t, git.ToolCall)
	assert.Equal(t, "toolu_git", git.ToolCall.Call.ToolUseID)

	require.Len(t, open.Roots, 1, "TextEdit is not a command of its own")
	assert.Equal(t, int64(5), open.Roots[0].Children[0].Children[0].ID, "TextEdit is under the call's open")
	assert.Equal(t, []int64{3, 4, 5}, open.ProcessIDs)
	assert.Equal(t, []int64{5}, got.Inferred)
	assert.Empty(t, got.Services)
	assert.False(t, got.Truncated)
	assert.Equal(t, "47abb2a1", reader.filter.SessionID, "the calls are the session's")
}

// spec:server-rest-api/agent-session-commands/commands-page-in-the-order-they-started
func TestAgentSessionCommands_Pages(t *testing.T) {
	t.Parallel()
	srv := newCommandsServer(t, fakeService{buildScopedTree: commandsTree(true)}, commandsCalls(), summarySession())

	status, first := getCommands(t, srv, "?limit=2")
	require.Equal(t, http.StatusOK, status)
	require.Len(t, first.Commands, 2)
	assert.Equal(t, 3, first.TotalMatched)
	assert.True(t, first.Truncated, "the session's process read was cut")
	assert.Equal(t, []int64{5}, first.Inferred, "the page's inferred launch")
	require.NotEmpty(t, first.NextCursor)

	status, second := getCommands(t, srv, "?limit=2&cursor="+first.NextCursor)
	require.Equal(t, http.StatusOK, status)
	require.Len(t, second.Commands, 1)
	assert.Equal(t, "git status", second.Commands[0].Command)
	assert.Empty(t, second.NextCursor)
	assert.Empty(t, second.Inferred, "no inferred launch on this page")

	status, _ = getCommands(t, srv, "?cursor=not-a-cursor")
	assert.Equal(t, http.StatusBadRequest, status)
}

// spec:server-rest-api/agent-session-commands/a-query-keeps-the-commands-it-finds
func TestAgentSessionCommands_Query(t *testing.T) {
	t.Parallel()
	srv := newCommandsServer(t, fakeService{buildScopedTree: commandsTree(false)}, commandsCalls(), summarySession())
	status, got := getCommands(t, srv, "?q=TEXTEDIT")
	require.Equal(t, http.StatusOK, status)
	require.Len(t, got.Commands, 1)
	assert.Equal(t, "open -a TextEdit", got.Commands[0].Command)
	assert.Equal(t, 1, got.TotalMatched, "the total is the query's")
}

func TestAgentSessionCommands_Failures(t *testing.T) {
	t.Parallel()
	treeFails := func(context.Context, string, api.TreeScope, int, bool, int64) (api.ProcessTreeResult, error) {
		return api.ProcessTreeResult{}, errors.New("db down")
	}
	for _, tc := range []struct {
		name   string
		tree   func(context.Context, string, api.TreeScope, int, bool, int64) (api.ProcessTreeResult, error)
		reader *fakeToolCallReader
		lister AgentSessionLister
		want   int
	}{
		{"an unknown session", commandsTree(false), commandsCalls(), &fakeSessionLister{}, http.StatusNotFound},
		{"the process read fails", treeFails, commandsCalls(), summarySession(), http.StatusInternalServerError},
		{"the tool-call read fails", commandsTree(false), &fakeToolCallReader{err: errors.New("down")}, summarySession(),
			http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newCommandsServer(t, fakeService{buildScopedTree: tc.tree}, tc.reader, tc.lister)
			status, _ := getCommands(t, srv, "")
			assert.Equal(t, tc.want, status)
		})
	}
}

// buildTree is a session that ran one long build: its wrapper shell, make, and make's workers. With workers past the cap, the last
// worker opened an app, which launchd started (row 9000) without the session's marker, and the link moves under that worker.
func buildTree(workers int) func(context.Context, string, api.TreeScope, int, bool, int64) (api.ProcessTreeResult, error) {
	return func(context.Context, string, api.TreeScope, int, bool, int64) (api.ProcessTreeResult, error) {
		var children []api.ProcessNode
		for i := range workers {
			id := int64(10 + i)
			children = append(children, commandProc(id, 1000+i, int64(2000+i), "/usr/bin/cc", []string{"cc", "worker-" + strconv.Itoa(i)}))
		}
		makeProc := commandProc(2, 101, 1001, "/usr/bin/make", []string{"make", "-j"}, children...)
		roots := []api.ProcessNode{toolCallShell(1, 100, 1000, "make -j", makeProc)}
		var links []api.ProcessLink
		if workers > 100 {
			roots = append(roots, commandProc(9000, 5000, 9000, "/Applications/Preview.app/Contents/MacOS/Preview", []string{"Preview"}))
			links = []api.ProcessLink{{From: int64(10 + workers - 1), To: 9000, Kind: api.ProcessLinkInferredLaunch}}
		}
		return api.ProcessTreeResult{Roots: roots, Links: links}, nil
	}
}

// listedIDs are the rows a command's forest lists, three levels deep: the wrapper, make, and make's workers.
func listedIDs(roots []struct {
	ID       int64 `json:"id"`
	Children []struct {
		ID       int64 `json:"id"`
		Children []struct {
			ID int64 `json:"id"`
		} `json:"children"`
	} `json:"children"`
}) []int64 {
	var ids []int64
	for _, r := range roots {
		ids = append(ids, r.ID)
		for _, c := range r.Children {
			ids = append(ids, c.ID)
			for _, g := range c.Children {
				ids = append(ids, g.ID)
			}
		}
	}
	return ids
}

// spec:server-rest-api/session-commands-list-their-processes-in-part/a-large-command-lists-its-first-processes-and-its-alerted-ones
// spec:server-rest-api/session-commands-list-their-processes-in-part/one-command-is-read-whole-on-request
func TestAgentSessionCommands_ListALargeCommandInPart(t *testing.T) {
	t.Parallel()
	const workers = 150
	alertedWorker := int64(10 + 140)
	lister := summarySession()
	const launched = int64(9000)
	lister.alerted = []int64{alertedWorker, launched}
	srv := newCommandsServer(t, fakeService{buildScopedTree: buildTree(workers)}, commandsCalls(), lister)

	status, got := getCommands(t, srv, "")
	require.Equal(t, http.StatusOK, status)
	require.Len(t, got.Commands, 1)
	build := got.Commands[0]
	assert.True(t, build.ProcessesCut)
	assert.Len(t, build.ProcessIDs, workers+3, "every process is still named, the launched app among them")
	assert.Contains(t, lister.askedAlerts, launched, "alerts are asked for by row, so a launch without the session's marker counts")
	listed := listedIDs(build.Roots)
	assert.Contains(t, listed, alertedWorker)
	assert.Len(t, listed, 100+2, "the first hundred, the alerted worker, and the last worker the alerted app is listed under")
	assert.Contains(t, listed, int64(10+workers-1), "the alerted app's launcher is listed, so the app is")

	status, searched := getCommands(t, srv, "?q=worker-145")
	require.Equal(t, http.StatusOK, status)
	require.Len(t, searched.Commands, 1)
	assert.Contains(t, listedIDs(searched.Commands[0].Roots), int64(10+145), "a search hit past the first hundred is listed")

	status, whole := getCommands(t, srv, "?command="+build.Key)
	require.Equal(t, http.StatusOK, status)
	require.Len(t, whole.Commands, 1)
	assert.False(t, whole.Commands[0].ProcessesCut)
	assert.Len(t, listedIDs(whole.Commands[0].Roots), workers+2, "one command asked for is listed whole, three levels of it here")
	assert.Equal(t, 1, whole.TotalMatched)

	status, _ = getCommands(t, srv, "?command=row:999")
	assert.Equal(t, http.StatusNotFound, status, "a command the session does not hold")
}

func TestAgentSessionCommands_AlertReadFailureFailsThePage(t *testing.T) {
	t.Parallel()
	lister := summarySession()
	srv := newCommandsServer(t, fakeService{buildScopedTree: buildTree(3)}, commandsCalls(), &alertFailingLister{lister})
	status, _ := getCommands(t, srv, "")
	assert.Equal(t, http.StatusInternalServerError, status)
}

// alertFailingLister reads sessions but fails to list their alerted processes.
type alertFailingLister struct{ *fakeSessionLister }

func (alertFailingLister) AlertedProcesses(context.Context, []int64) ([]int64, error) {
	return nil, errors.New("mysql down")
}
