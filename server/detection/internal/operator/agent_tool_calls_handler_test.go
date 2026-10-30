package operator

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

type fakeToolCallReader struct {
	filter    visibilityapi.AgentToolCallFilter
	calls     []visibilityapi.AgentToolCall
	truncated bool
	lineage   visibilityapi.AgentSessionLineage
	asked     *lineageAsk
	err       error
	// prompts answers AgentPrompts, promptsErr fails it, and promptFilter is the filter it was asked with.
	prompts      visibilityapi.AgentPromptPage
	promptsErr   error
	promptFilter visibilityapi.AgentPromptFilter
	// found answers SearchAgentPrompts, searchErr fails it, and search is what it was asked.
	found     visibilityapi.FoundAgentPromptPage
	searchErr error
	search    *visibilityapi.AgentPromptSearch
}

func (f *fakeToolCallReader) SearchAgentPrompts(
	_ context.Context, search visibilityapi.AgentPromptSearch,
) (visibilityapi.FoundAgentPromptPage, error) {
	f.search = &search
	return f.found, f.searchErr
}

// lineageAsk is the lineage read a handler made.
type lineageAsk struct {
	hostID, sessionID string
	ownerUID          int64
}

func (f *fakeToolCallReader) AgentSessionLineage(
	_ context.Context, hostID, sessionID string, ownerUID int64,
) (visibilityapi.AgentSessionLineage, error) {
	f.asked = &lineageAsk{hostID, sessionID, ownerUID}
	return f.lineage, f.err
}

func (f *fakeToolCallReader) AgentToolCalls(
	_ context.Context, filter visibilityapi.AgentToolCallFilter,
) ([]visibilityapi.AgentToolCall, bool, error) {
	f.filter = filter
	return f.calls, f.truncated, f.err
}

func (f *fakeToolCallReader) AgentPrompts(_ context.Context, filter visibilityapi.AgentPromptFilter) (visibilityapi.AgentPromptPage, error) {
	f.promptFilter = filter
	return f.prompts, f.promptsErr
}

func newToolCallsServer(t *testing.T, lister AgentSessionLister, reader AgentToolCallReader) *httptest.Server {
	t.Helper()
	h := New(fakeService{}, allowAllAuthZ{}, slog.Default())
	if lister != nil {
		h.SetAgentSessions(lister)
	}
	if reader != nil {
		h.SetAgentToolCalls(reader)
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

const (
	toolCallsPath = "/api/hosts/h1/agent-sessions/47abb2a1/tool-calls"
	lineagePath   = "/api/hosts/h1/agent-sessions/47abb2a1/lineage"
)

// spec:server-rest-api/agent-session-lineage-endpoint/a-session-names-its-parent-and-its-forks
func TestAgentSessionLineage_NamesParentAndForks(t *testing.T) {
	t.Parallel()
	owned := func(session string, uid int64) api.AgentSessionSummary {
		return api.AgentSessionSummary{HostID: "h1", AgentSessionID: session, OwnerUID: new(uid)}
	}
	lister := &fakeSessionLister{result: api.AgentSessionResult{Rows: []api.AgentSessionSummary{
		owned("47abb2a1", 501), owned("5828cd24", 501), owned("bd46ed99", 501), owned("c1a2b3c4", 501),
	}}}
	reader := &fakeToolCallReader{lineage: visibilityapi.AgentSessionLineage{
		ForkedFromSessionID: "5828cd24", ForkedSessionIDs: []string{"bd46ed99", "c1a2b3c4"},
	}}
	resp := doGet(t, newToolCallsServer(t, lister, reader), lineagePath)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, map[string]any{"forked_from_session_id": "5828cd24", "forked_session_ids": []any{"bd46ed99", "c1a2b3c4"}}, out)
	assert.Equal(t, &lineageAsk{"h1", "47abb2a1", 501}, reader.asked, "read for the session's owner")

	// spec:server-rest-api/agent-session-lineage-endpoint/another-user-s-transcripts-do-not-make-a-lineage
	t.Run("a link to a session another user owns, or the host does not hold, is left out", func(t *testing.T) {
		t.Parallel()
		lister := &fakeSessionLister{result: api.AgentSessionResult{Rows: []api.AgentSessionSummary{
			owned("47abb2a1", 502), owned("5828cd24", 501), owned("bd46ed99", 502), owned("c1a2b3c4", 501),
		}}}
		// The 502 owner's transcripts name 501's sessions as this session's parent and fork: only its own fork stays.
		reader := &fakeToolCallReader{lineage: visibilityapi.AgentSessionLineage{
			ForkedFromSessionID: "5828cd24", ForkedSessionIDs: []string{"bd46ed99", "c1a2b3c4", "unheld"},
		}}
		resp := doGet(t, newToolCallsServer(t, lister, reader), lineagePath)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var out map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		assert.Equal(t, map[string]any{"forked_session_ids": []any{"bd46ed99"}}, out)
	})
	t.Run("a session with no retained process has no owner to read a lineage for", func(t *testing.T) {
		t.Parallel()
		unowned := &fakeSessionLister{result: api.AgentSessionResult{Rows: []api.AgentSessionSummary{{HostID: "h1", AgentSessionID: "47abb2a1"}}}}
		reader := &fakeToolCallReader{lineage: visibilityapi.AgentSessionLineage{ForkedFromSessionID: "5828cd24"}}
		resp := doGet(t, newToolCallsServer(t, unowned, reader), lineagePath)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var out map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		assert.Equal(t, map[string]any{"forked_session_ids": []any{}}, out)
		assert.Nil(t, reader.asked)
	})
}

// spec:server-rest-api/agent-tool-calls-endpoint/a-session-s-tool-calls-are-listed-once-each
func TestAgentToolCalls_ListsTheSessionsCalls(t *testing.T) {
	t.Parallel()
	session := api.AgentSessionSummary{HostID: "h1", AgentSessionID: "47abb2a1", FirstSeenNs: 5 * int64(time.Hour), LastSeenNs: 6 * int64(time.Hour)}
	lister := &fakeSessionLister{result: api.AgentSessionResult{Rows: []api.AgentSessionSummary{session}}}
	reader := &fakeToolCallReader{
		calls:     []visibilityapi.AgentToolCall{{ToolUseID: "toolu_a", ToolName: "Bash", Command: "ls", CalledAtNs: 5 * int64(time.Hour)}},
		truncated: true,
	}
	resp := doGet(t, newToolCallsServer(t, lister, reader), toolCallsPath)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out agentToolCallsResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, reader.calls, out.ToolCalls)
	assert.True(t, out.Truncated)
	assert.Equal(t, api.AgentSessionFilter{HostID: "h1", AgentSessionID: "47abb2a1"}, lister.filter)
	// The session's span, widened by an hour on each side for a call made before its process and for the user's approval.
	assert.Equal(t, visibilityapi.AgentToolCallFilter{
		HostID: "h1", SessionID: "47abb2a1", FromNs: 4 * int64(time.Hour), ToNs: 7 * int64(time.Hour), Limit: maxToolCalls,
	}, reader.filter)
}

func TestAgentToolCalls_Errors(t *testing.T) {
	t.Parallel()
	// known is a fresh lister holding the session for each run: the fakes record what they were asked, so runs in parallel do not
	// share one.
	known := func() AgentSessionLister {
		return &fakeSessionLister{result: api.AgentSessionResult{Rows: []api.AgentSessionSummary{
			{HostID: "h1", AgentSessionID: "47abb2a1", OwnerUID: new(int64(501))},
		}}}
	}
	cases := []struct {
		name   string
		lister func() AgentSessionLister
		reader func() AgentToolCallReader
		want   int
	}{
		{name: "unwired", lister: func() AgentSessionLister { return nil }, reader: func() AgentToolCallReader { return nil },
			want: http.StatusServiceUnavailable},
		{name: "an unknown session", lister: func() AgentSessionLister { return &fakeSessionLister{} },
			reader: func() AgentToolCallReader { return &fakeToolCallReader{} }, want: http.StatusNotFound},
		{name: "the session read fails", lister: func() AgentSessionLister { return &fakeSessionLister{err: errors.New("db")} },
			reader: func() AgentToolCallReader { return &fakeToolCallReader{} }, want: http.StatusInternalServerError},
		{name: "the archive read fails", lister: known,
			reader: func() AgentToolCallReader { return &fakeToolCallReader{err: errors.New("ch")} }, want: http.StatusInternalServerError},
	}
	for _, path := range []string{toolCallsPath, lineagePath} {
		for _, tc := range cases {
			t.Run(path+" "+tc.name, func(t *testing.T) {
				t.Parallel()
				resp := doGet(t, newToolCallsServer(t, tc.lister(), tc.reader()), path)
				defer resp.Body.Close()
				assert.Equal(t, tc.want, resp.StatusCode)
			})
		}
	}

	t.Run("no calls is an empty list", func(t *testing.T) {
		t.Parallel()
		resp := doGet(t, newToolCallsServer(t, known(), &fakeToolCallReader{}), toolCallsPath)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var raw map[string]json.RawMessage
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&raw))
		assert.JSONEq(t, `[]`, string(raw["tool_calls"]))
	})
}
