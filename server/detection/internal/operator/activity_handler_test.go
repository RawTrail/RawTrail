package operator

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/internal/graph"
	identityapi "github.com/rawtrail/rawtrail/server/identity/api"
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

type fakeActivityReader struct {
	filter *visibilityapi.ActivityFilter
	result visibilityapi.ActivityRollup
	err    error
	// page is what ActivityPage answers; class and cursor are what it was asked for.
	page          visibilityapi.ActivityPage
	class, cursor string
	// touches are what SessionTouches answers, by pid; touchesErr fails it. touchReads counts its calls.
	touches    map[int64][]visibilityapi.SessionTouch
	touchesErr error
	touchReads atomic.Int32
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
	f.touchReads.Add(1)
	return f.touches[generation.PID], f.touchesErr
}

func (f *fakeActivityReader) ActivityPage(
	_ context.Context, filter visibilityapi.ActivityFilter, class, cursor string,
) (visibilityapi.ActivityPage, error) {
	f.filter, f.class, f.cursor = &filter, class, cursor
	return f.page, f.err
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

// rollupNaming is a rollup whose network list holds a row of each generation, so its response names those processes.
func rollupNaming(generations ...visibilityapi.ProcessGeneration) visibilityapi.ActivityRollup {
	var rollup visibilityapi.ActivityRollup
	for _, g := range generations {
		rollup.Network.Rows = append(rollup.Network.Rows, visibilityapi.NetworkActivityRow{
			ActivityRow: visibilityapi.ActivityRow{PID: g.PID, PIDVersion: g.PIDVersion},
		})
	}
	return rollup
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
	reader := &fakeActivityReader{
		processes: map[int64]api.Process{2: {ID: 2, HostID: "h1", PID: 20, PIDVersion: &pv, Path: "/bin/zsh"}},
		result:    rollupNaming(visibilityapi.ProcessGeneration{PID: 20, PIDVersion: 200}),
	}
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
	result := rollupNaming(visibilityapi.ProcessGeneration{PID: 20, PIDVersion: 200}, visibilityapi.ProcessGeneration{PID: 30, PIDVersion: 300})
	result.Network.ActivityTotals = visibilityapi.ActivityTotals{Events: 5, Distinct: 2}
	reader := &fakeActivityReader{result: result}
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
		// limit is how many of the scope's processes the read asks for: a whole session (issue #1424), a story's chain cap.
		limit int
	}{
		{query: "scope=session&session=169b5bb1", want: api.TreeScope{AgentSessionID: "169b5bb1"}, limit: graph.MaxScopedRead},
		{query: "scope=story&root=875325", want: api.TreeScope{StoryRootID: 875325}, limit: maxTimelineChain},
	} {
		t.Run(tc.query, func(t *testing.T) {
			t.Parallel()
			var got api.TreeScope
			readScope := func(_ context.Context, _ string, scope api.TreeScope, limit int, flatten bool, _ int64) (api.ProcessTreeResult, error) {
				assert.True(t, flatten)
				assert.Equal(t, tc.limit, limit)
				got = scope
				res := activityChain()
				res.Truncated = true
				return res, nil
			}
			svc := fakeService{buildScopedTree: readScope}
			reader := &fakeActivityReader{result: rollupNaming(
				visibilityapi.ProcessGeneration{PID: 10, PIDVersion: 100}, visibilityapi.ProcessGeneration{PID: 20, PIDVersion: 200},
				visibilityapi.ProcessGeneration{PID: 30, PIDVersion: 300},
			)}
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
		reader := &fakeActivityReader{processes: processes, result: rollupNaming(
			visibilityapi.ProcessGeneration{PID: int64(agentPID), PIDVersion: 9}, visibilityapi.ProcessGeneration{PID: 5001, PIDVersion: 1},
		)}
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

// spec:server-rest-api/activity-rollup-bounded-to-its-scope-s-start/an-agent-bounded-to-a-later-session-bounds-it-from-that-session
// spec:server-rest-api/agent-activity-bounded-to-its-session/a-reused-agent-counts-in-each-session-only-while-serving-it
func TestActivityHandler_SessionBoundsTheAgent(t *testing.T) {
	t.Parallel()
	agentRow, agentPID, agentVersion := int64(50), 4911, uint32(9)
	// The agent started long before this session (its fork at 100), the session's shell at 700.
	agentProc := api.Process{ID: agentRow, HostID: "h1", PID: agentPID, PIDVersion: &agentVersion, Path: "/opt/claude/2.1.296", ForkTimeNs: 100}
	sessionID := "07c2d66d"
	shell := activityNode(1, 5001, 1)
	shell.ForkTimeNs = 700
	shell.ParentProcessID = &agentRow
	shell.AgentSessionID = &sessionID
	// A program the session had launchd run is a root without the session's marker too, but it is not the agent: it stays whole even
	// when it touched session paths of its own.
	launched := activityNode(2, 6001, 2)
	launched.ForkTimeNs = 710
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
	assert.Equal(t, int64(673), reader.filter.FromNs, "the read starts at the agent's span, not at the agent's own start")

	failing := &fakeActivityReader{processes: map[int64]api.Process{agentRow: agentProc}, touchesErr: errors.New("ch")}
	assert.Equal(t, http.StatusInternalServerError, serve(failing))

	// A session over the generation cap is cut from its tail, and the agent is not what is cut.
	crowded := []api.ProcessNode{shell}
	for i := range graph.MaxScopedRead {
		crowded = append(crowded, activityNode(int64(100+i), 10_000+i, 1))
	}
	capped := &fakeActivityReader{processes: reader.processes, touches: reader.touches}
	require.Equal(t, http.StatusOK, serveRoots(capped, crowded))
	assert.Len(t, capped.filter.Generations, graph.MaxScopedRead-1, "the agent's generation is the one the cap made room for")
	assert.Equal(t, reader.filter.Spans, capped.filter.Spans, "a crowded session still bounds its agent")
}

// spec:server-rest-api/an-agent-s-session-touches-are-kept-briefly/a-session-s-reads-look-up-its-agent-s-touches-once
//
// The totals and each class's page of one session look up its agent's session touches once between them, and a failed lookup is not
// kept.
func TestActivityHandler_SessionReadsShareTheAgentTouches(t *testing.T) {
	t.Parallel()
	agentRow, agentPID, agentVersion := int64(50), 4911, uint32(9)
	sessionID := "07c2d66d"
	shell := activityNode(1, 5001, 1)
	shell.ParentProcessID = &agentRow
	shell.AgentSessionID = &sessionID
	readScope := func(context.Context, string, api.TreeScope, int, bool, int64) (api.ProcessTreeResult, error) {
		return api.ProcessTreeResult{Roots: []api.ProcessNode{shell}}, nil
	}
	sessions := api.AgentSessionResult{Rows: []api.AgentSessionSummary{{HostID: "h1", AgentSessionID: sessionID, AgentPID: &agentPID}}}
	agentProc := api.Process{ID: agentRow, HostID: "h1", PID: agentPID, PIDVersion: &agentVersion, Path: "/opt/claude/2.1.296"}
	serve := func(reader *fakeActivityReader) []int {
		h := New(fakeService{buildScopedTree: readScope}, allowAllAuthZ{}, slog.Default())
		h.SetActivity(reader)
		h.SetAgentSessions(&fakeSessionLister{result: sessions})
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)
		var statuses []int
		scope := "?scope=session&session=" + sessionID
		for _, path := range []string{"/api/hosts/h1/activity" + scope + "&rows=false", "/api/hosts/h1/activity/files" + scope,
			"/api/hosts/h1/activity/network" + scope, "/api/hosts/h1/activity/dns" + scope} {
			resp := doGet(t, srv, path)
			_ = resp.Body.Close()
			statuses = append(statuses, resp.StatusCode)
		}
		return statuses
	}

	reader := &fakeActivityReader{
		processes: map[int64]api.Process{agentRow: agentProc},
		touches: map[int64][]visibilityapi.SessionTouch{
			int64(agentPID): {{SessionID: "4f5b9ea2", FirstNs: 633}, {SessionID: sessionID, FirstNs: 673}},
		},
	}
	assert.Equal(t, []int{http.StatusOK, http.StatusOK, http.StatusOK, http.StatusOK}, serve(reader))
	assert.Equal(t, int32(1), reader.touchReads.Load())
	require.NotNil(t, reader.filter)
	assert.Len(t, reader.filter.Spans, 1, "the kept touches still bound the agent")

	failing := &fakeActivityReader{processes: map[int64]api.Process{agentRow: agentProc}, touchesErr: errors.New("ch")}
	statuses := serve(failing)
	assert.Equal(t, http.StatusInternalServerError, statuses[0])
	assert.EqualValues(t, len(statuses), failing.touchReads.Load(), "a failed lookup is read again")
}

// timedNode is a process row with only its times: its fork, its recorded start if any, and whether a snapshot found it.
func timedNode(forkNs int64, startNs *int64, snapshot bool) api.ProcessNode {
	var n api.ProcessNode
	n.ForkTimeNs, n.StartTimeNs, n.IsSnapshot = forkNs, startNs, snapshot
	return n
}

// spec:server-rest-api/activity-rollup-bounded-to-its-scope-s-start/an-earlier-process-sharing-a-generation-is-not-counted
// spec:server-rest-api/activity-rollup-bounded-to-its-scope-s-start/a-scope-whose-start-is-not-known-is-read-unbounded
func TestScopeStart(t *testing.T) {
	t.Parallel()
	start := func(ns int64) *int64 { return &ns }
	cases := []struct {
		name  string
		procs []api.ProcessNode
		want  int64
	}{
		{"earliest fork of the scope", []api.ProcessNode{timedNode(500, nil, false), timedNode(300, nil, false)}, 300},
		{"a start before its fork wins", []api.ProcessNode{timedNode(500, start(200), false), timedNode(300, nil, false)}, 200},
		{"a snapshot's recorded start bounds it", []api.ProcessNode{timedNode(900, start(100), true), timedNode(300, nil, false)}, 100},
		{"a snapshot with no start leaves it unbounded", []api.ProcessNode{timedNode(300, nil, false), timedNode(900, nil, true)}, 0},
		{"a process with no time leaves it unbounded", []api.ProcessNode{timedNode(300, nil, false), timedNode(0, nil, false)}, 0},
		{"an empty scope is unbounded", nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, scopeStart(tc.procs, nil))
		})
	}
}

// spec:server-rest-api/activity-rollup-bounded-to-its-scope-s-start/an-agent-bounded-to-a-later-session-bounds-it-from-that-session
func TestScopeStart_AgentBoundedToASpan(t *testing.T) {
	t.Parallel()
	agent := timedNode(100, nil, false)
	agent.PID, agent.PIDVersion = 21989, new(uint32(38549))
	gen := visibilityapi.ProcessGeneration{PID: 21989, PIDVersion: 38549}
	spanFrom := func(fromNs int64) []visibilityapi.GenerationSpan {
		return []visibilityapi.GenerationSpan{{ProcessGeneration: gen, FromNs: fromNs}}
	}
	child := timedNode(5000, nil, false)
	cases := []struct {
		name  string
		procs []api.ProcessNode
		spans []visibilityapi.GenerationSpan
		want  int64
	}{
		{"the agent's span start, not its own start", []api.ProcessNode{agent, child}, spanFrom(4000), 4000},
		{"a child that started earlier still bounds it", []api.ProcessNode{agent, timedNode(3000, nil, false)}, spanFrom(4000), 3000},
		{"an agent serving its first session counts from its start", []api.ProcessNode{agent, child}, spanFrom(0), 100},
		{"another generation's span does not move it", []api.ProcessNode{agent, child},
			[]visibilityapi.GenerationSpan{{ProcessGeneration: visibilityapi.ProcessGeneration{PID: 21989, PIDVersion: 1}, FromNs: 4000}}, 100},
		{"an agent with no generation counts from its start", []api.ProcessNode{timedNode(100, nil, false), child}, spanFrom(4000), 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, scopeStart(tc.procs, tc.spans))
		})
	}
}

// spec:server-rest-api/activity-rollup-bounded-to-its-scope-s-start/an-earlier-process-sharing-a-generation-is-not-counted
func TestActivityHandler_ReadsFromTheScopeStart(t *testing.T) {
	t.Parallel()
	chain := activityChain()
	pinned := &chain.Roots[0].Children[0]
	pinned.ForkTimeNs = 2000
	pinned.Children[0].ForkTimeNs = 3000
	pinned.Children[1].ForkTimeNs = 1000 // no generation, so not read, and not what bounds the read
	chain.Roots[0].ForkTimeNs = 500      // the ancestor, outside the subtree
	readChain := func(context.Context, string, api.TimeRange, int64, bool) (api.ProcessTreeResult, error) {
		return chain, nil
	}
	reader := &fakeActivityReader{}
	status, _ := getActivity(t, newActivityServer(t, fakeService{buildChainTree: readChain}, reader, allowAllAuthZ{}),
		"scope=subtree&process=2")
	require.Equal(t, http.StatusOK, status)
	require.NotNil(t, reader.filter)
	assert.Equal(t, int64(2000), reader.filter.FromNs, "the earliest fork among the processes the rollup reads")
}

// A response names only the processes its rows were matched by: a long session's scope is 20,000 processes, and listing each made its
// Activity response 2 MB of process names (issue #1550). ?rows=false reads the totals alone.
//
// spec:server-rest-api/activity-reads-load-in-parts/a-response-names-the-processes-of-its-rows
func TestActivityHandler_NamesTheProcessesOfItsRows(t *testing.T) {
	t.Parallel()
	readChain := func(context.Context, string, api.TimeRange, int64, bool) (api.ProcessTreeResult, error) {
		return activityChain(), nil
	}
	svc := fakeService{buildChainTree: readChain}
	named := visibilityapi.ProcessGeneration{PID: 30, PIDVersion: 300}

	t.Run("the rollup", func(t *testing.T) {
		t.Parallel()
		reader := &fakeActivityReader{result: rollupNaming(named)}
		status, out := getActivity(t, newActivityServer(t, svc, reader, allowAllAuthZ{}), "scope=subtree&process=2")
		require.Equal(t, http.StatusOK, status)
		assert.Len(t, generationsOf(reader.filter), 2, "the scope is the subtree's")
		assert.Equal(t, []activityProcess{{ID: 3, PID: 30, PIDVersion: 300, Path: "/bin/pd"}}, out.Processes)
		assert.False(t, reader.filter.TotalsOnly)
	})
	t.Run("a DNS row names each process that resolved its name", func(t *testing.T) {
		t.Parallel()
		var rollup visibilityapi.ActivityRollup
		rollup.DNS.Rows = []visibilityapi.DNSActivityRow{{
			ActivityRow: visibilityapi.ActivityRow{PID: 20, PIDVersion: 200}, QueryName: "api.github.com", ProcessCount: 2,
			Resolvers: []visibilityapi.DNSResolver{{PID: 20, PIDVersion: 200}, {PID: 30, PIDVersion: 300}},
		}}
		reader := &fakeActivityReader{result: rollup}
		status, out := getActivity(t, newActivityServer(t, svc, reader, allowAllAuthZ{}), "scope=subtree&process=2")
		require.Equal(t, http.StatusOK, status)
		var ids []int64
		for _, p := range out.Processes {
			ids = append(ids, p.ID)
		}
		assert.Equal(t, []int64{2, 3}, ids)
	})
	t.Run("a page", func(t *testing.T) {
		t.Parallel()
		// The page's scope is process 2 (pid 20, pidversion 200), which getActivityPage stores.
		reader := &fakeActivityReader{page: visibilityapi.ActivityPage{DNS: []visibilityapi.DNSActivityRow{{
			ActivityRow: visibilityapi.ActivityRow{PID: 20, PIDVersion: 200}, QueryName: "example.com",
			Resolvers: []visibilityapi.DNSResolver{{PID: 20, PIDVersion: 200}}, ProcessCount: 1,
		}}}}
		status, out := getActivityPage(t, reader, "/api/hosts/h1/activity/dns?scope=process&process=2")
		require.Equal(t, http.StatusOK, status)
		assert.JSONEq(t, `[{"id":2,"pid":20,"pidversion":200,"path":"/bin/zsh"}]`, string(out["processes"]))

		reader.page = visibilityapi.ActivityPage{}
		status, out = getActivityPage(t, reader, "/api/hosts/h1/activity/dns?scope=process&process=2")
		require.Equal(t, http.StatusOK, status)
		assert.JSONEq(t, `[]`, string(out["processes"]), "a page of no rows names no process")
	})
	t.Run("the totals alone", func(t *testing.T) {
		t.Parallel()
		reader := &fakeActivityReader{}
		status, out := getActivity(t, newActivityServer(t, svc, reader, allowAllAuthZ{}), "scope=subtree&process=2&rows=false")
		require.Equal(t, http.StatusOK, status)
		assert.True(t, reader.filter.TotalsOnly)
		assert.Equal(t, timelineScope{Applied: true, Processes: 3, Unmatched: 1}, out.Scope)
		assert.Empty(t, out.Processes)
	})
}

// The Activity tab's totals and its three lists, read beside each other, share the session's read with each other and the session's
// other views (issue #1550).
//
// spec:server-rest-api/activity-reads-load-in-parts/a-session-s-activity-reads-share-its-process-read
func TestActivityHandler_SessionReadsShareTheSessionRead(t *testing.T) {
	t.Parallel()
	var reads atomic.Int32
	svc := fakeService{buildScopedTree: func(context.Context, string, api.TreeScope, int, bool, int64) (api.ProcessTreeResult, error) {
		reads.Add(1)
		return activityChain(), nil
	}}
	h := New(svc, allowAllAuthZ{}, slog.Default())
	h.SetActivity(&fakeActivityReader{})
	h.SetAgentToolCalls(commandsCalls())
	h.SetAgentSessions(&fakeSessionLister{result: api.AgentSessionResult{Rows: []api.AgentSessionSummary{
		{HostID: "h1", AgentSessionID: "47abb2a1", LastSeenNs: 5},
	}}})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	const scope = "?scope=session&session=47abb2a1"
	for _, path := range []string{
		"/api/hosts/h1/activity" + scope + "&rows=false", "/api/hosts/h1/activity/files" + scope,
		"/api/hosts/h1/activity/network" + scope, "/api/hosts/h1/activity/dns" + scope, "/api/hosts/h1/agent-sessions/47abb2a1/processes",
	} {
		resp := doGet(t, srv, path)
		_ = resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode, path)
	}
	assert.Equal(t, int32(1), reads.Load())
}
