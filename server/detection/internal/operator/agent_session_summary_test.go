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
	identityapi "github.com/rawtrail/rawtrail/server/identity/api"
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// fakeSessionTotals answers the summary's reads and records the filters they were made with.
type fakeSessionTotals struct {
	counts       api.AgentSessionCounts
	activity     visibilityapi.ActivitySummary
	tools        []visibilityapi.ToolCallCount
	countsErr    error
	activityErr  error
	toolsErr     error
	counted      []string
	activityRead *visibilityapi.ActivityFilter
	toolsRead    *visibilityapi.AgentToolCallFilter
	usage        visibilityapi.AgentUsageTotals
	usageErr     error
	usageRead    *visibilityapi.AgentToolCallFilter
}

func (f *fakeSessionTotals) AgentSessionCounts(_ context.Context, hostID, sessionID string) (api.AgentSessionCounts, error) {
	f.counted = []string{hostID, sessionID}
	return f.counts, f.countsErr
}

func (f *fakeSessionTotals) ActivityTotalsForGenerations(
	_ context.Context, filter visibilityapi.ActivityFilter,
) (visibilityapi.ActivitySummary, error) {
	f.activityRead = &filter
	return f.activity, f.activityErr
}

func (f *fakeSessionTotals) AgentToolCallCounts(
	_ context.Context, filter visibilityapi.AgentToolCallFilter,
) ([]visibilityapi.ToolCallCount, error) {
	f.toolsRead = &filter
	return f.tools, f.toolsErr
}

func (f *fakeSessionTotals) AgentUsageTotals(_ context.Context, filter visibilityapi.AgentToolCallFilter) (visibilityapi.AgentUsageTotals, error) {
	f.usageRead = &filter
	return f.usage, f.usageErr
}

const summaryPath = "/api/hosts/h1/agent-sessions/47abb2a1/summary"

// summarySession is the session the summary tests read: no agent pid, so its scope is its own processes alone.
func summarySession() *fakeSessionLister {
	return &fakeSessionLister{result: api.AgentSessionResult{Rows: []api.AgentSessionSummary{
		{HostID: "h1", AgentSessionID: "47abb2a1", FirstSeenNs: 10_000_000_000_000, LastSeenNs: 20_000_000_000_000},
	}}}
}

// summaryTree is the session's processes as the scoped read returns them: two tool calls, one with a child.
func summaryTree(truncated bool) func(context.Context, string, api.TreeScope, int, bool, int64) (api.ProcessTreeResult, error) {
	return func(_ context.Context, _ string, scope api.TreeScope, _ int, _ bool, _ int64) (api.ProcessTreeResult, error) {
		if scope.AgentSessionID != "47abb2a1" {
			return api.ProcessTreeResult{}, nil
		}
		return api.ProcessTreeResult{Roots: []api.ProcessNode{
			activityNode(1, 501, 1, activityNode(3, 503, 3)), activityNode(2, 502, 2),
		}, Truncated: truncated}, nil
	}
}

func newSummaryServer(
	t *testing.T, svc fakeService, az identityapi.AuthZ, lister AgentSessionLister, totals AgentSessionTotalsReader,
) *httptest.Server {
	t.Helper()
	h := New(svc, az, slog.Default())
	h.SetAgentSessions(lister)
	h.SetAgentToolCalls(&fakeToolCallReader{})
	h.SetActivity(&fakeActivityReader{})
	if totals != nil {
		h.SetAgentSessionTotals(totals)
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func getSummary(t *testing.T, srv *httptest.Server) (int, agentSessionSummaryResponse) {
	t.Helper()
	resp := doGet(t, srv, summaryPath)
	defer resp.Body.Close()
	var out agentSessionSummaryResponse
	if resp.StatusCode == http.StatusOK {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	}
	return resp.StatusCode, out
}

// spec:server-rest-api/agent-session-summary/a-session-is-totalled
func TestAgentSessionSummary_Totals(t *testing.T) {
	t.Parallel()
	totals := &fakeSessionTotals{
		counts: api.AgentSessionCounts{Processes: 3, AlertsBySeverity: map[string]int64{api.SeverityHigh: 2, api.SeverityCritical: 1}},
		activity: visibilityapi.ActivitySummary{
			Files: visibilityapi.FileTotals{
				ClassTotals: visibilityapi.ClassTotals{Events: 6, Distinct: 4}, ByOperation: map[string]int64{"modify": 5, "create": 1},
			},
			Network: visibilityapi.ClassTotals{Events: 2, Distinct: 1},
			DNS:     visibilityapi.ClassTotals{Events: 1, Distinct: 1},
		},
		tools: []visibilityapi.ToolCallCount{{ToolName: "Read", Count: 4}, {ToolName: "Bash", Count: 2}},
		usage: visibilityapi.AgentUsageTotals{
			Models: []visibilityapi.ModelUsage{{Model: "claude-opus-5-5", Messages: 2, Input: 6, Output: 300, CacheRead: 9000}},
			Modes:  []visibilityapi.ModeUsage{{Mode: "bypassPermissions", Messages: 2, FirstNs: 100, LastNs: 200}},
		},
	}
	// Counted from the server's goroutine and read by the test, so atomically.
	var reads atomic.Int64
	tree := summaryTree(false)
	counted := func(ctx context.Context, host string, scope api.TreeScope, limit int, flatten bool, pin int64) (api.ProcessTreeResult, error) {
		reads.Add(1)
		return tree(ctx, host, scope, limit, flatten, pin)
	}
	srv := newSummaryServer(t, fakeService{buildScopedTree: counted}, allowAllAuthZ{}, summarySession(), totals)

	status, got := getSummary(t, srv)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, int64(1), reads.Load(), "one read of the session's processes gives its commands and its activity scope")
	assert.Equal(t, agentSessionSummaryResponse{
		Commands:  2,
		Processes: 3,
		Alerts:    alertTotals{Total: 3, BySeverity: map[string]int64{api.SeverityHigh: 2, api.SeverityCritical: 1}},
		ToolCalls: toolCallTotals{Total: 6, ByTool: totals.tools},
		Activity:  totals.activity,
		Scope:     timelineScope{Applied: true, Processes: 3},
		Usage:     totals.usage,
	}, got)

	assert.Equal(t, []string{"h1", "47abb2a1"}, totals.counted)
	require.NotNil(t, totals.activityRead)
	assert.Equal(t, []visibilityapi.ProcessGeneration{{PID: 501, PIDVersion: 1}, {PID: 503, PIDVersion: 3}, {PID: 502, PIDVersion: 2}},
		totals.activityRead.Generations, "the activity is counted over the session's processes, as its rollup reads them")
	require.NotNil(t, totals.toolsRead)
	assert.Equal(t, visibilityapi.AgentToolCallFilter{
		HostID: "h1", SessionID: "47abb2a1", Limit: maxToolCalls,
		FromNs: 10_000_000_000_000 - toolCallMargin.Nanoseconds(), ToNs: 20_000_000_000_000 + toolCallMargin.Nanoseconds(),
	}, *totals.toolsRead, "the tool calls are counted over the window the session's tool-call read uses")
	require.NotNil(t, totals.usageRead)
	assert.Equal(t, *totals.toolsRead, *totals.usageRead, "the tokens are totalled over the session the tool calls are")
}

// The summary counts the commands its session's commands list lists: two tool calls, the second without a child.
//
// spec:server-rest-api/agent-session-summary-command-total/the-summary-counts-what-the-commands-list-lists
func TestAgentSessionSummary_CountsCommands(t *testing.T) {
	t.Parallel()
	totals := &fakeSessionTotals{counts: api.AgentSessionCounts{AlertsBySeverity: map[string]int64{}}}
	svc := fakeService{buildScopedTree: summaryTree(false)}
	srv := newSummaryServer(t, svc, allowAllAuthZ{}, summarySession(), totals)
	status, got := getSummary(t, srv)
	require.Equal(t, http.StatusOK, status)

	commandsSrv := newCommandsServer(t, svc, &fakeToolCallReader{}, summarySession())
	status, listed := getCommands(t, commandsSrv, "")
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, listed.TotalMatched, got.Commands)
	assert.Equal(t, 2, got.Commands)
}

// spec:server-rest-api/agent-session-summary/the-summary-reports-the-reach-of-its-activity
func TestAgentSessionSummary_ReportsReach(t *testing.T) {
	t.Parallel()
	totals := &fakeSessionTotals{counts: api.AgentSessionCounts{AlertsBySeverity: map[string]int64{}}}
	srv := newSummaryServer(t, fakeService{buildScopedTree: summaryTree(true)}, allowAllAuthZ{}, summarySession(), totals)

	status, got := getSummary(t, srv)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, timelineScope{Applied: true, Processes: 3, Truncated: true}, got.Scope)
	assert.Len(t, totals.activityRead.Generations, 3, "counted over the generations read")
}

// spec:server-rest-api/agent-session-summary/an-unknown-session-has-no-summary
func TestAgentSessionSummary_UnknownSession(t *testing.T) {
	t.Parallel()
	totals := &fakeSessionTotals{}
	srv := newSummaryServer(t, fakeService{buildScopedTree: summaryTree(false)}, allowAllAuthZ{}, &fakeSessionLister{}, totals)

	status, _ := getSummary(t, srv)
	assert.Equal(t, http.StatusNotFound, status)
	assert.Nil(t, totals.counted, "nothing is counted")
	assert.Nil(t, totals.activityRead, "nothing is counted")
	assert.Nil(t, totals.toolsRead, "nothing is counted")
}

func TestAgentSessionSummary_Failures(t *testing.T) {
	t.Parallel()
	boom := errors.New("db down")
	scopeFails := func(context.Context, string, api.TreeScope, int, bool, int64) (api.ProcessTreeResult, error) {
		return api.ProcessTreeResult{}, boom
	}
	for _, tc := range []struct {
		name   string
		az     identityapi.AuthZ
		tree   func(context.Context, string, api.TreeScope, int, bool, int64) (api.ProcessTreeResult, error)
		totals AgentSessionTotalsReader
		want   int
	}{
		{name: "no process read", az: denyAllAuthZ{}, totals: &fakeSessionTotals{}, want: http.StatusForbidden},
		{name: "no totals reader", az: allowAllAuthZ{}, want: http.StatusServiceUnavailable},
		{name: "counts fail", az: allowAllAuthZ{}, totals: &fakeSessionTotals{countsErr: boom}, want: http.StatusInternalServerError},
		{name: "scope fails", az: allowAllAuthZ{}, tree: scopeFails, totals: &fakeSessionTotals{}, want: http.StatusInternalServerError},
		{name: "activity fails", az: allowAllAuthZ{}, totals: &fakeSessionTotals{activityErr: boom}, want: http.StatusInternalServerError},
		{name: "tool calls fail", az: allowAllAuthZ{}, totals: &fakeSessionTotals{toolsErr: boom}, want: http.StatusInternalServerError},
		{name: "usage fails", az: allowAllAuthZ{}, totals: &fakeSessionTotals{usageErr: boom}, want: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tree := tc.tree
			if tree == nil {
				tree = summaryTree(false)
			}
			srv := newSummaryServer(t, fakeService{buildScopedTree: tree}, tc.az, summarySession(), tc.totals)
			status, _ := getSummary(t, srv)
			assert.Equal(t, tc.want, status)
		})
	}
}
