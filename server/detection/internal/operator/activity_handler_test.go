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

	"github.com/fleetdm/edr/server/detection/api"
	identityapi "github.com/fleetdm/edr/server/identity/api"
	visibilityapi "github.com/fleetdm/edr/server/visibility/api"
)

type fakeActivityReader struct {
	filter *visibilityapi.ActivityFilter
	result visibilityapi.ActivityRollup
	err    error
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
func TestActivityHandler_ProcessAndSubtree(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		query   string
		want    []visibilityapi.ProcessGeneration
		scope   timelineScope
		process []int64
	}{
		{
			name:  "the process alone, without its ancestors or children",
			query: "scope=process&process=2",
			want:  []visibilityapi.ProcessGeneration{{PID: 20, PIDVersion: 200}},
			scope: timelineScope{Applied: true, Processes: 1}, process: []int64{2},
		},
		{
			name:  "the process and its descendants, without its ancestors",
			query: "scope=subtree&process=2",
			want:  []visibilityapi.ProcessGeneration{{PID: 20, PIDVersion: 200}, {PID: 30, PIDVersion: 300}},
			scope: timelineScope{Applied: true, Processes: 3, Unmatched: 1}, process: []int64{2, 3},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var gotPin int64
			readChain := func(_ context.Context, hostID string, _ api.TimeRange, pin int64, flatten bool) (api.ProcessTreeResult, error) {
				assert.Equal(t, "h1", hostID)
				assert.True(t, flatten, "generations are read from the raw forest")
				gotPin = pin
				return activityChain(), nil
			}
			svc := fakeService{buildChainTree: readChain}
			reader := &fakeActivityReader{result: visibilityapi.ActivityRollup{Network: visibilityapi.NetworkActivity{
				ActivityTotals: visibilityapi.ActivityTotals{Events: 5, Distinct: 2},
			}}}
			status, out := getActivity(t, newActivityServer(t, svc, reader, allowAllAuthZ{}), tc.query)
			require.Equal(t, http.StatusOK, status)
			assert.Equal(t, int64(2), gotPin)
			assert.Equal(t, tc.want, generationsOf(reader.filter))
			assert.Equal(t, "h1", reader.filter.HostID)
			assert.Equal(t, tc.scope, out.Scope)
			var ids []int64
			for _, p := range out.Processes {
				ids = append(ids, p.ID)
			}
			assert.Equal(t, tc.process, ids)
			assert.Equal(t, int64(5), out.Network.Events, "the rollup is passed through")
		})
	}
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

// A process the chain read does not hold is a scope of nothing, not the host's whole activity.
func TestActivityHandler_UnknownProcessTotalsNothing(t *testing.T) {
	t.Parallel()
	svc := fakeService{buildChainTree: func(context.Context, string, api.TimeRange, int64, bool) (api.ProcessTreeResult, error) {
		return activityChain(), nil
	}}
	reader := &fakeActivityReader{}
	status, out := getActivity(t, newActivityServer(t, svc, reader, allowAllAuthZ{}), "scope=process&process=99")
	require.Equal(t, http.StatusOK, status)
	assert.Empty(t, generationsOf(reader.filter))
	assert.Equal(t, timelineScope{}, out.Scope)
	assert.NotNil(t, out.Processes)
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
		{name: "tree read fails", svc: failingTree, reader: reader(), az: allow, query: "scope=process&process=2",
			want: http.StatusInternalServerError},
		{name: "archive read fails", svc: chain, reader: failingReader, az: allow, query: "scope=process&process=2",
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
