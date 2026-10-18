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
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// The timeline scopes (issue #1282) resolve a chain, session or story with the service's tree reads and filter the real in-memory
// archive to its generations. The service is faked to return a fixed forest, so these pin the handler's resolution and report.

func newScopedTimelineServer(t *testing.T, svc fakeService, archive visibilityapi.EventArchive) *httptest.Server {
	t.Helper()
	h := New(svc, allowAllAuthZ{}, slog.Default())
	h.SetHostTimeline(archive)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func treeNode(pid int, pidversion *uint32, children ...api.ProcessNode) api.ProcessNode {
	return api.ProcessNode{Process: api.Process{PID: pid, PIDVersion: pidversion}, Children: children}
}

// scopeForest is a chain of three generations (pids 1, 2 and 3) and a fourth process (pid 4) with no generation.
func scopeForest() api.ProcessTreeResult {
	return api.ProcessTreeResult{Roots: []api.ProcessNode{
		treeNode(1, new(uint32(10)), treeNode(2, new(uint32(20)), treeNode(3, new(uint32(30))), treeNode(4, nil))),
	}}
}

func scopedArchive(t *testing.T) visibilityapi.EventArchive {
	t.Helper()
	return timelineArchive(t,
		ev("in-1", "h1", 100, "exec", `{"pid":1,"pidversion":10,"path":"/bin/zsh"}`),
		ev("in-2", "h1", 200, "network_connect", `{"pid":2,"pidversion":20,"remote_address":"1.2.3.4"}`),
		ev("in-3", "h1", 300, "exec", `{"pid":3,"pidversion":30,"path":"/bin/sleep"}`),
		ev("reused", "h1", 400, "exec", `{"pid":2,"pidversion":99,"path":"/usr/bin/python3"}`),
		ev("no-gen", "h1", 500, "exec", `{"pid":4,"path":"/bin/legacy"}`),
		ev("other", "h1", 600, "exec", `{"pid":7,"pidversion":70,"path":"/usr/libexec/other"}`),
	)
}

type scopedTimelineResponse struct {
	Events []struct {
		EventID string `json:"event_id"`
	} `json:"events"`
	Scope *timelineScope `json:"scope"`
}

func getScopedTimeline(t *testing.T, srv *httptest.Server, query string) (int, scopedTimelineResponse) {
	t.Helper()
	resp := doGet(t, srv, "/api/hosts/h1/timeline?"+query)
	defer resp.Body.Close()
	var out scopedTimelineResponse
	if resp.StatusCode == http.StatusOK {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	}
	return resp.StatusCode, out
}

func eventIDs(out scopedTimelineResponse) []string {
	var ids []string
	for _, e := range out.Events {
		ids = append(ids, e.EventID)
	}
	return ids
}

// spec:server-rest-api/host-timeline-scopes/the-alert-chain-is-resolved-by-the-server
func TestHostTimelineScope_Chain(t *testing.T) {
	t.Parallel()
	var gotPin int64
	var gotTo int64
	svc := fakeService{buildChainTree: func(_ context.Context, hostID string, tr api.TimeRange, pin int64, flatten bool) (api.ProcessTreeResult, error) {
		assert.Equal(t, "h1", hostID)
		assert.True(t, flatten, "generations are read from the raw forest, never from an aggregate")
		gotPin, gotTo = pin, tr.ToNs
		return scopeForest(), nil
	}}
	srv := newScopedTimelineServer(t, svc, scopedArchive(t))

	status, out := getScopedTimeline(t, srv, "scope=chain&pin=42&to=1000")
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, int64(42), gotPin)
	assert.Equal(t, int64(1000), gotTo, "the timeline's window end bounds the chain's descendants")
	assert.ElementsMatch(t, []string{"in-1", "in-2", "in-3"}, eventIDs(out), "the reused pid and the unrelated process stay out")
	assert.Equal(t, &timelineScope{Applied: true, Processes: 4, Unmatched: 1}, out.Scope)
}

// spec:server-rest-api/host-timeline-scopes/a-session-or-story-scopes-the-timeline
func TestHostTimelineScope_SessionAndStory(t *testing.T) {
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
			var gotLimit int
			svc := fakeService{buildScopedTree: func(_ context.Context, _ string, scope api.TreeScope, limit int, flatten bool, _ int64) (api.ProcessTreeResult, error) {
				assert.True(t, flatten, "generations are read from the raw forest, never from an aggregate")
				got, gotLimit = scope, limit
				res := scopeForest()
				res.Truncated = true
				return res, nil
			}}
			srv := newScopedTimelineServer(t, svc, scopedArchive(t))
			status, out := getScopedTimeline(t, srv, tc.query)
			require.Equal(t, http.StatusOK, status)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, maxTimelineChain, gotLimit)
			assert.ElementsMatch(t, []string{"in-1", "in-2", "in-3"}, eventIDs(out))
			assert.Equal(t, &timelineScope{Applied: true, Processes: 4, Unmatched: 1, Truncated: true}, out.Scope)
		})
	}
}

// spec:server-rest-api/host-timeline-scopes/a-scope-with-nothing-to-match-shows-the-whole-host
func TestHostTimelineScope_NothingToMatch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		forest api.ProcessTreeResult
		want   timelineScope
	}{
		{name: "no stored process", forest: api.ProcessTreeResult{}, want: timelineScope{}},
		{name: "processes without a generation", forest: api.ProcessTreeResult{Roots: []api.ProcessNode{treeNode(4, nil)}},
			want: timelineScope{Processes: 1, Unmatched: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := fakeService{buildChainTree: func(context.Context, string, api.TimeRange, int64, bool) (api.ProcessTreeResult, error) {
				return tc.forest, nil
			}}
			srv := newScopedTimelineServer(t, svc, scopedArchive(t))
			status, out := getScopedTimeline(t, srv, "scope=chain&pin=42")
			require.Equal(t, http.StatusOK, status)
			assert.Len(t, out.Events, 6, "the whole host's stream")
			assert.Equal(t, &tc.want, out.Scope)
		})
	}
}

// A chain read whose descendants come from the story index can return more generations than the timeline's filter takes.
// The cap keeps the chain's top: its root and the processes nearest it, which include the pinned process, survive the cut.
func TestHostTimelineScope_TruncatesPastTheFilterCap(t *testing.T) {
	t.Parallel()
	var descendants []api.ProcessNode
	for i := range maxTimelineChain + 1 {
		descendants = append(descendants, treeNode(10_000+i, new(uint32(1000+i))))
	}
	svc := fakeService{buildChainTree: func(context.Context, string, api.TimeRange, int64, bool) (api.ProcessTreeResult, error) {
		return api.ProcessTreeResult{Roots: []api.ProcessNode{treeNode(1, new(uint32(10)), descendants...)}}, nil
	}}
	srv := newScopedTimelineServer(t, svc, scopedArchive(t))
	status, out := getScopedTimeline(t, srv, "scope=chain&pin=42")
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, &timelineScope{Applied: true, Processes: maxTimelineChain + 2, Truncated: true}, out.Scope)
	assert.Equal(t, []string{"in-1"}, eventIDs(out), "the root's events survive the cut")
}

// spec:server-rest-api/host-timeline-scopes/a-malformed-timeline-scope-is-rejected
func TestHostTimelineScope_Rejected(t *testing.T) {
	t.Parallel()
	srv := newScopedTimelineServer(t, fakeService{}, scopedArchive(t))
	for _, query := range []string{
		"scope=chain", "scope=chain&pin=0", "scope=chain&pin=x", "scope=session", "scope=story&root=0",
		"scope=chain&pin=42&chain=1:10",
	} {
		status, _ := getScopedTimeline(t, srv, query)
		assert.Equal(t, http.StatusBadRequest, status, query)
	}
}

// Without a scope the response carries no report, and ?chain= keeps working as before.
func TestHostTimelineScope_AbsentWithoutAScope(t *testing.T) {
	t.Parallel()
	srv := newScopedTimelineServer(t, fakeService{}, scopedArchive(t))
	status, out := getScopedTimeline(t, srv, "chain=1:10")
	require.Equal(t, http.StatusOK, status)
	assert.Nil(t, out.Scope)
	assert.Equal(t, []string{"in-1"}, eventIDs(out))
}
