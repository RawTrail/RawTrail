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
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

type fakeActivityReader struct {
	filter *visibilityapi.ActivityFilter
	result visibilityapi.ActivityRollup
	err    error
	// touches are what SessionTouches answers, by pid; touchesErr fails it.
	touches    map[int64][]visibilityapi.SessionTouch
	touchesErr error
	// processes are the rows GetProcessByID answers from, by id; processErr fails it.
	processes  map[int64]api.Process
	processErr error
}

func (f *fakeActivityReader) GetProcessByID(_ context.Context, hostID string, id int64) (*api.Process, error) {
	if f.processErr != nil {
		return nil, f.processErr
	}
	p, ok := f.processes[id]
	if !ok || p.HostID != hostID {
		return nil, nil
	}
	return &p, nil
}

func (f *fakeActivityReader) SessionTouches(
	_ context.Context, _ string, generation visibilityapi.ProcessGeneration,
) ([]visibilityapi.SessionTouch, error) {
	return f.touches[generation.PID], f.touchesErr
}

func (f *fakeActivityReader) ActivityForGenerations(
	_ context.Context, filter visibilityapi.ActivityFilter,
) (visibilityapi.ActivityRollup, error) {
	f.filter = &filter
	return f.result, f.err
}

func newActivityServer(t *testing.T, svc fakeService, reader ActivityReader, az identityapi.AuthZ) *httptest.Server {
	t.Helper()
	h := New(svc, az, slog.Default())
	if reader != nil {
		h.SetActivity(reader)
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// activityNode is a process row with an id, a pid and, unless pidversion is 0, a generation.
func activityNode(id int64, pid int, pidversion uint32, children ...api.ProcessNode) api.ProcessNode {
	n := api.ProcessNode{Process: api.Process{ID: id, PID: pid, Path: "/bin/p" + string(rune('a'+id%26))}, Children: children}
	if pidversion != 0 {
		n.PIDVersion = &pidversion
	}
	return n
}

// activityChain is an ancestor (id 1), the pinned process (id 2), and its two children (ids 3 and 4), the last with no generation.
func activityChain() api.ProcessTreeResult {
	return api.ProcessTreeResult{Roots: []api.ProcessNode{
		activityNode(1, 10, 100, activityNode(2, 20, 200, activityNode(3, 30, 300), activityNode(4, 40, 0))),
	}}
}

func getActivity(t *testing.T, srv *httptest.Server, query string) (int, activityResponse) {
	t.Helper()
	resp := doGet(t, srv, "/api/hosts/h1/activity?"+query)
	defer resp.Body.Close()
	var out activityResponse
	if resp.StatusCode == http.StatusOK {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	}
	return resp.StatusCode, out
}

func generationsOf(f *visibilityapi.ActivityFilter) []visibilityapi.ProcessGeneration {
	if f == nil {
		return nil
	}
	return f.Generations
}

// spec:server-rest-api/process-activity-rollup/a-process-or-its-subtree-is-totalled
func TestActivityHandler_ProcessAlone(t *testing.T) {
	t.Parallel()
	// The process alone is one row by its id: no tree read at all.
	svc := fakeService{buildChainTree: func(context.Context, string, api.TimeRange, int64, bool) (api.ProcessTreeResult, error) {
		t.Error("a process scope must not read the chain")
		return api.ProcessTreeResult{}, nil
	}}
	pv := uint32(200)
	reader := &fakeActivityReader{processes: map[int64]api.Process{2: {ID: 2, HostID: "h1", PID: 20, PIDVersion: &pv, Path: "/bin/zsh"}}}
	status, out := getActivity(t, newActivityServer(t, svc, reader, allowAllAuthZ{}), "scope=process&process=2")
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, []visibilityapi.ProcessGeneration{{PID: 20, PIDVersion: 200}}, generationsOf(reader.filter))
	assert.Equal(t, timelineScope{Applied: true, Processes: 1}, out.Scope)
	assert.Equal(t, []activityProcess{{ID: 2, PID: 20, PIDVersion: 200, Path: "/bin/zsh"}}, out.Processes)
}

// spec:server-rest-api/process-activity-rollup/a-process-or-its-subtree-is-totalled
func TestActivityHandler_Subtree(t *testing.T) {
	t.Parallel()
	var gotPin int64
	readChain := func(_ context.Context, hostID string, _ api.TimeRange, pin int64, flatten bool) (api.ProcessTreeResult, error) {
		assert.Equal(t, "h1", hostID)
		assert.True(t, flatten, "generations are read from the raw forest")
		gotPin = pin
		return activityChain(), nil
	}
	reader := &fakeActivityReader{result: visibilityapi.ActivityRollup{Network: visibilityapi.NetworkActivity{
		ActivityTotals: visibilityapi.ActivityTotals{Events: 5, Distinct: 2},
	}}}
	srv := newActivityServer(t, fakeService{buildChainTree: readChain}, reader, allowAllAuthZ{})
	status, out := getActivity(t, srv, "scope=subtree&process=2")
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, int64(2), gotPin)
	// The process and its descendants, without its ancestor; the child without a generation is counted as unmatched.
	assert.Equal(t, []visibilityapi.ProcessGeneration{{PID: 20, PIDVersion: 200}, {PID: 30, PIDVersion: 300}}, generationsOf(reader.filter))
	assert.Equal(t, "h1", reader.filter.HostID)
	assert.Equal(t, timelineScope{Applied: true, Processes: 3, Unmatched: 1}, out.Scope)
	var ids []int64
	for _, p := range out.Processes {
		ids = append(ids, p.ID)
	}
	assert.Equal(t, []int64{2, 3}, ids)
	assert.Equal(t, int64(5), out.Network.Events, "the rollup is passed through")
}

// spec:server-rest-api/process-activity-rollup/a-session-or-story-is-totalled
func TestActivityHandler_SessionAndStory(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		query string
		want  api.TreeScope
	}{
		{query: "scope=session&session=169b5bb1", want: api.TreeScope{AgentSessionID: "169b5bb1"}},
		{query: "scope=story&root=875325", want: api.TreeScope{StoryRootID: 875325}},
	} {
		t.Run(tc.query, func(t *testing.T) {
			t.Parallel()
			var got api.TreeScope
			readScope := func(_ context.Context, _ string, scope api.TreeScope, limit int, flatten bool, _ int64) (api.ProcessTreeResult, error) {
				assert.True(t, flatten)
				assert.Equal(t, maxTimelineChain, limit)
				got = scope
				res := activityChain()
				res.Truncated = true
				return res, nil
			}
			svc := fakeService{buildScopedTree: readScope}
			reader := &fakeActivityReader{}
			status, out := getActivity(t, newActivityServer(t, svc, reader, allowAllAuthZ{}), tc.query)
			require.Equal(t, http.StatusOK, status)
			assert.Equal(t, tc.want, got)
			assert.Len(t, generationsOf(reader.filter), 3, "every process of the scope that carries a generation")
			assert.Equal(t, timelineScope{Applied: true, Processes: 4, Unmatched: 1, Truncated: true}, out.Scope)
			assert.Len(t, out.Processes, 3)
			assert.Equal(t, uint32(300), out.Processes[2].PIDVersion)
		})
	}
}

// A session's activity includes the agent itself, which the session read never holds: Claude Code's Edit runs in the claude process,
// the creator of each tool call's shell (issue #1287).
//
// spec:server-rest-api/agent-file-activity-reads/a-session-s-activity-includes-the-agent-s-own-edits
func TestActivityHandler_SessionIncludesTheAgent(t *testing.T) {
	t.Parallel()
	agentRow, otherCreator := int64(50), int64(60)
	shell := func(id int64, pid int, pidversion uint32, creator int64) api.ProcessNode {
		n := activityNode(id, pid, pidversion)
		n.ParentProcessID = &creator
		return n
	}
	agentPID := 4911
	sessions := api.AgentSessionResult{Rows: []api.AgentSessionSummary{{HostID: "h1", AgentSessionID: "47abb2a1", AgentPID: &agentPID}}}
	processes := map[int64]api.Process{
		agentRow:     {ID: agentRow, HostID: "h1", PID: agentPID, Path: "/Users/dev/.local/share/claude/versions/2.1.296"},
		otherCreator: {ID: otherCreator, HostID: "h1", PID: 777, Path: "/bin/zsh"},
	}
	// Both creators carry a generation, so only the pid decides which joins the scope.
	agentVersion, otherVersion := uint32(9), uint32(70)
	agent, other := processes[agentRow], processes[otherCreator]
	agent.PIDVersion, other.PIDVersion = &agentVersion, &otherVersion
	processes[agentRow], processes[otherCreator] = agent, other
	readScope := func(context.Context, string, api.TreeScope, int, bool, int64) (api.ProcessTreeResult, error) {
		// Two tool calls created by the agent, and a root a marked process outside the agent created.
		roots := []api.ProcessNode{shell(1, 5001, 1, agentRow), shell(2, 5002, 2, agentRow), shell(3, 5003, 3, otherCreator)}
		return api.ProcessTreeResult{Roots: roots}, nil
	}

	t.Run("the agent joins the scope once", func(t *testing.T) {
		t.Parallel()
		reader := &fakeActivityReader{processes: processes}
		h := New(fakeService{buildScopedTree: readScope}, allowAllAuthZ{}, slog.Default())
		h.SetActivity(reader)
		h.SetAgentSessions(&fakeSessionLister{result: sessions})
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)
		status, out := getActivity(t, srv, "scope=session&session=47abb2a1")
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, []visibilityapi.ProcessGeneration{
			{PID: int64(agentPID), PIDVersion: 9}, {PID: 5001, PIDVersion: 1}, {PID: 5002, PIDVersion: 2}, {PID: 5003, PIDVersion: 3},
		}, generationsOf(reader.filter))
		assert.Equal(t, agentRow, out.Processes[0].ID)
	})

	for _, tc := range []struct {
		name   string
		reader *fakeActivityReader
		lister *fakeSessionLister
	}{
		{
			name:   "a session read failure",
			reader: &fakeActivityReader{processes: processes},
			lister: &fakeSessionLister{err: errors.New("db down")},
		},
		{
			name:   "a creator read failure",
			reader: &fakeActivityReader{processErr: errors.New("db down")},
			lister: &fakeSessionLister{result: sessions},
		},
	} {
		t.Run(tc.name+" fails the read", func(t *testing.T) {
			t.Parallel()
			h := New(fakeService{buildScopedTree: readScope}, allowAllAuthZ{}, slog.Default())
			h.SetActivity(tc.reader)
			h.SetAgentSessions(tc.lister)
			mux := http.NewServeMux()
			h.RegisterRoutes(mux)
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)
			status, _ := getActivity(t, srv, "scope=session&session=47abb2a1")
			assert.Equal(t, http.StatusInternalServerError, status)
		})
	}
}

// A process that is not stored, or that belongs to another host, is a scope of nothing, not the host's whole activity.
func TestActivityHandler_UnknownProcessTotalsNothing(t *testing.T) {
	t.Parallel()
	svc := fakeService{buildChainTree: func(context.Context, string, api.TimeRange, int64, bool) (api.ProcessTreeResult, error) {
		return activityChain(), nil
	}}
	pv := uint32(1)
	for _, query := range []string{"scope=process&process=99", "scope=process&process=7", "scope=subtree&process=99"} {
		t.Run(query, func(t *testing.T) {
			t.Parallel()
			reader := &fakeActivityReader{processes: map[int64]api.Process{7: {ID: 7, HostID: "other-host", PID: 70, PIDVersion: &pv}}}
			status, out := getActivity(t, newActivityServer(t, svc, reader, allowAllAuthZ{}), query)
			require.Equal(t, http.StatusOK, status)
			assert.Empty(t, generationsOf(reader.filter))
			assert.Equal(t, timelineScope{}, out.Scope)
			assert.NotNil(t, out.Processes)
		})
	}
}

// spec:server-rest-api/process-activity-rollup/a-malformed-activity-scope-is-rejected
func TestActivityHandler_Errors(t *testing.T) {
	t.Parallel()
	chain := fakeService{buildChainTree: func(context.Context, string, api.TimeRange, int64, bool) (api.ProcessTreeResult, error) {
		return activityChain(), nil
	}}
	failingTree := fakeService{buildChainTree: func(context.Context, string, api.TimeRange, int64, bool) (api.ProcessTreeResult, error) {
		return api.ProcessTreeResult{}, errors.New("boom")
	}}
	reader := func() ActivityReader { return &fakeActivityReader{} }
	failingReader := &fakeActivityReader{err: errors.New("boom")}
	allow := allowAllAuthZ{}
	for _, tc := range []struct {
		name   string
		svc    fakeService
		reader ActivityReader
		az     identityapi.AuthZ
		query  string
		want   int
	}{
		{name: "no scope", svc: chain, reader: reader(), az: allow, query: "", want: http.StatusBadRequest},
		{name: "process without an id", svc: chain, reader: reader(), az: allow, query: "scope=subtree", want: http.StatusBadRequest},
		{name: "process with a bad id", svc: chain, reader: reader(), az: allow, query: "scope=process&process=0", want: http.StatusBadRequest},
		{name: "a chain is not an activity scope", svc: chain, reader: reader(), az: allow, query: "scope=chain&pin=2",
			want: http.StatusBadRequest},
		{name: "session without an id", svc: chain, reader: reader(), az: allow, query: "scope=session", want: http.StatusBadRequest},
		{name: "tree read fails", svc: failingTree, reader: reader(), az: allow, query: "scope=subtree&process=2",
			want: http.StatusInternalServerError},
		{name: "process read fails", svc: chain, reader: &fakeActivityReader{processErr: errors.New("boom")}, az: allow,
			query: "scope=process&process=2", want: http.StatusInternalServerError},
		{name: "archive read fails", svc: chain, reader: failingReader, az: allow, query: "scope=subtree&process=2",
			want: http.StatusInternalServerError},
		{name: "surface not wired", svc: chain, az: allow, query: "scope=process&process=2", want: http.StatusServiceUnavailable},
		{name: "process read not granted", svc: chain, reader: reader(), az: denyAllAuthZ{}, query: "scope=process&process=2",
			want: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			status, _ := getActivity(t, newActivityServer(t, tc.svc, tc.reader, tc.az), tc.query)
			assert.Equal(t, tc.want, status)
		})
	}
}

// spec:server-rest-api/agent-activity-bounded-to-its-session/a-reused-agent-counts-in-each-session-only-while-serving-it
func TestSessionSpan(t *testing.T) {
	t.Parallel()
	// The edr-dev capture: one claude served 4f5b9ea2, then 07c2d66d after /clear; a third session is added for the middle case.
	touches := []visibilityapi.SessionTouch{
		{SessionID: "4f5b9ea2", FirstNs: 633}, {SessionID: "07c2d66d", FirstNs: 673}, {SessionID: "9a1c", FirstNs: 900},
	}
	for _, tc := range []struct {
		name    string
		touches []visibilityapi.SessionTouch
		session string
		want    visibilityapi.GenerationSpan
		bounded bool
	}{
		{"the first session keeps the process's start", touches, "4f5b9ea2", visibilityapi.GenerationSpan{ToNs: 673}, true},
		{"a middle session runs between its first touch and the next", touches, "07c2d66d",
			visibilityapi.GenerationSpan{FromNs: 673, ToNs: 900}, true},
		{"the last session is open-ended", touches, "9a1c", visibilityapi.GenerationSpan{FromNs: 900}, true},
		{"one session in the process is not bounded", touches[:1], "4f5b9ea2", visibilityapi.GenerationSpan{}, false},
		{"a session whose transcript was never seen is not bounded", touches, "missing", visibilityapi.GenerationSpan{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, bounded := sessionSpan(tc.touches, tc.session)
			assert.Equal(t, tc.bounded, bounded)
			assert.Equal(t, tc.want, got)
		})
	}
}

// spec:server-rest-api/agent-activity-bounded-to-its-session/a-reused-agent-counts-in-each-session-only-while-serving-it
func TestActivityHandler_SessionBoundsTheAgent(t *testing.T) {
	t.Parallel()
	agentRow, agentPID, agentVersion := int64(50), 4911, uint32(9)
	agentProc := api.Process{ID: agentRow, HostID: "h1", PID: agentPID, PIDVersion: &agentVersion, Path: "/opt/claude/2.1.296"}
	sessionID := "07c2d66d"
	shell := activityNode(1, 5001, 1)
	shell.ParentProcessID = &agentRow
	shell.AgentSessionID = &sessionID
	// A program the session had launchd run is a root without the session's marker too, but it is not the agent: it stays whole even
	// when it touched session paths of its own.
	launched := activityNode(2, 6001, 2)
	sessions := api.AgentSessionResult{Rows: []api.AgentSessionSummary{{HostID: "h1", AgentSessionID: sessionID, AgentPID: &agentPID}}}
	serveRoots := func(reader *fakeActivityReader, roots []api.ProcessNode) int {
		readScope := func(context.Context, string, api.TreeScope, int, bool, int64) (api.ProcessTreeResult, error) {
			return api.ProcessTreeResult{Roots: roots}, nil
		}
		h := New(fakeService{buildScopedTree: readScope}, allowAllAuthZ{}, slog.Default())
		h.SetActivity(reader)
		h.SetAgentSessions(&fakeSessionLister{result: sessions})
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)
		status, _ := getActivity(t, srv, "scope=session&session="+sessionID)
		return status
	}
	serve := func(reader *fakeActivityReader) int { return serveRoots(reader, []api.ProcessNode{shell, launched}) }

	reader := &fakeActivityReader{
		processes: map[int64]api.Process{agentRow: agentProc},
		touches: map[int64][]visibilityapi.SessionTouch{
			int64(agentPID): {{SessionID: "4f5b9ea2", FirstNs: 633}, {SessionID: sessionID, FirstNs: 673}},
			6001:            {{SessionID: "4f5b9ea2", FirstNs: 640}, {SessionID: sessionID, FirstNs: 690}},
		},
	}
	require.Equal(t, http.StatusOK, serve(reader))
	require.NotNil(t, reader.filter)
	assert.Equal(t, []visibilityapi.ProcessGeneration{{PID: 5001, PIDVersion: 1}, {PID: 6001, PIDVersion: 2}}, reader.filter.Generations,
		"the session's own processes and what it launched stay whole")
	assert.Equal(t, []visibilityapi.GenerationSpan{
		{ProcessGeneration: visibilityapi.ProcessGeneration{PID: int64(agentPID), PIDVersion: 9}, FromNs: 673},
	}, reader.filter.Spans, "the agent counts only from its first touch of this session's transcript")

	failing := &fakeActivityReader{processes: map[int64]api.Process{agentRow: agentProc}, touchesErr: errors.New("ch")}
	assert.Equal(t, http.StatusInternalServerError, serve(failing))

	// A session over the generation cap is cut from its tail, and the agent is not what is cut.
	crowded := []api.ProcessNode{shell}
	for i := range maxTimelineChain {
		crowded = append(crowded, activityNode(int64(100+i), 10_000+i, 1))
	}
	capped := &fakeActivityReader{processes: reader.processes, touches: reader.touches}
	require.Equal(t, http.StatusOK, serveRoots(capped, crowded))
	assert.Len(t, capped.filter.Generations, maxTimelineChain-1, "the agent's generation is the one the cap made room for")
	assert.Equal(t, reader.filter.Spans, capped.filter.Spans, "a crowded session still bounds its agent")
}
