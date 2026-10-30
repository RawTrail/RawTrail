package operator

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identityapi "github.com/rawtrail/rawtrail/server/identity/api"
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

const promptSearchPath = "/api/agent-prompts"

// spec:server-rest-api/agent-prompt-search/a-search-finds-prompts-across-sessions-newest-first
// spec:server-rest-api/agent-prompt-search/each-search-is-audited-with-its-query
func TestSearchAgentPrompts_FindsAndAudits(t *testing.T) {
	t.Parallel()
	reader := &fakeToolCallReader{found: visibilityapi.FoundAgentPromptPage{
		Prompts: []visibilityapi.FoundAgentPrompt{{HostID: "h1", SessionID: "47abb2a1",
			AgentPrompt: visibilityapi.AgentPrompt{PromptID: "turn-1", Text: "deploy to prod", Length: 14, AtNs: 100, UID: 501}}},
		NextCursor: "c2",
	}}
	az := &promptGateAuthZ{allowPrompts: true}
	srv := newPromptsServer(t, az, reader)

	before := time.Now().UnixNano()
	resp := doGet(t, srv, promptSearchPath+"?q=+Deploy+&host_id=h1&limit=10")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"), "prompt text is never cached")
	var got visibilityapi.FoundAgentPromptPage
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, reader.found.Prompts, got.Prompts)
	require.NotEmpty(t, got.NextCursor)
	assert.NotEqual(t, "c2", got.NextCursor, "the archive's cursor travels inside the search's, with its window")

	require.NotNil(t, reader.search)
	asked := *reader.search
	assert.Equal(t, "Deploy", asked.Query, "the query is trimmed and keeps its case; the archive matches it regardless of case")
	assert.Equal(t, "h1", asked.HostID)
	assert.Empty(t, asked.Cursor)
	assert.Equal(t, 10, asked.Limit)
	assert.GreaterOrEqual(t, asked.ToNs, before, "with no window, the search ends now")
	assert.Equal(t, (7 * 24 * time.Hour).Nanoseconds(), asked.ToNs-asked.FromNs, "and looks back a week")
	assert.Equal(t, []identityapi.Resource{{Type: "agent_prompt_search", ID: "Deploy"}}, az.asked,
		"prompt read is asked for the search, named by its query, which the audit row records")

	// The next page, asked for later, reads the window the first resolved rather than a week ending at its own now.
	time.Sleep(2 * time.Millisecond)
	next := doGet(t, srv, promptSearchPath+"?q=Deploy&host_id=h1&limit=10&cursor="+got.NextCursor)
	defer next.Body.Close()
	require.Equal(t, http.StatusOK, next.StatusCode)
	require.NotNil(t, reader.search)
	assert.Equal(t, "c2", reader.search.Cursor, "the archive is given its own cursor back")
	assert.Equal(t, [2]int64{asked.FromNs, asked.ToNs}, [2]int64{reader.search.FromNs, reader.search.ToNs},
		"a later page keeps the first page's window")
}

// spec:server-rest-api/a-prompt-search-is-bounded/a-search-is-bounded
func TestSearchAgentPrompts_Rejects(t *testing.T) {
	t.Parallel()
	day := (24 * time.Hour).Nanoseconds()
	for _, tc := range []struct {
		name  string
		query string
		code  string
	}{
		{name: "no query", query: "", code: "invalid_query"},
		{name: "a query shorter than three characters", query: "?q=ab", code: "invalid_query"},
		{name: "a query longer than an audit target", query: "?q=" + strings.Repeat("x", 256), code: "invalid_query"},
		{name: "a query that is not UTF-8", query: "?q=%FF%FF%FF", code: "invalid_query"},
		{name: "a cursor that does not decode", query: "?q=deploy&cursor=not-a-cursor", code: "invalid_cursor"},
		{name: "a cursor missing its window", query: "?q=deploy&cursor=" + encodePromptSearchCursor(promptSearchBounds{}, "c2"),
			code: "invalid_cursor"},
		{name: "a cursor carrying a window wider than thirty days",
			query: "?q=deploy&cursor=" + encodePromptSearchCursor(promptSearchBounds{FromNs: 0, ToNs: 31 * day}, "c2"), code: "invalid_cursor"},
		{name: "a cursor carrying a negative bound",
			query: "?q=deploy&cursor=" + encodePromptSearchCursor(promptSearchBounds{FromNs: -1, ToNs: day}, "c2"), code: "invalid_cursor"},
		{name: "a cursor with no archive cursor",
			query: "?q=deploy&cursor=" + encodePromptSearchCursor(promptSearchBounds{FromNs: 1, ToNs: day}, ""), code: "invalid_cursor"},
		{name: "a window wider than thirty days", query: "?q=deploy&from=1&to=" + strconv.FormatInt(31*day, 10), code: "bad_window"},
		{name: "a window that ends before it starts", query: "?q=deploy&from=200&to=100", code: "bad_window"},
		{name: "a negative bound", query: "?q=deploy&from=-1", code: "bad_window"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reader := &fakeToolCallReader{}
			resp := doGet(t, newPromptsServer(t, &promptGateAuthZ{allowPrompts: true}, reader), promptSearchPath+tc.query)
			defer resp.Body.Close()
			require.Equal(t, http.StatusBadRequest, resp.StatusCode)
			var body struct {
				Error string `json:"error"`
			}
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
			assert.Equal(t, tc.code, body.Error)
			assert.Nil(t, reader.search, "nothing is read")
		})
	}

	t.Run("a window of exactly thirty days is read", func(t *testing.T) {
		t.Parallel()
		reader := &fakeToolCallReader{found: visibilityapi.FoundAgentPromptPage{Prompts: []visibilityapi.FoundAgentPrompt{}}}
		resp := doGet(t, newPromptsServer(t, &promptGateAuthZ{allowPrompts: true}, reader),
			promptSearchPath+"?q=deploy&from=1&to="+strconv.FormatInt(1+30*day, 10))
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.NotNil(t, reader.search)
		assert.Equal(t, [2]int64{1, 1 + 30*day}, [2]int64{reader.search.FromNs, reader.search.ToNs})
	})
}

func TestSearchAgentPrompts_Failures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		allow  bool
		reader *fakeToolCallReader
		want   int
	}{
		{name: "without prompt read", allow: false, reader: &fakeToolCallReader{}, want: http.StatusForbidden},
		{name: "a cursor that does not decode", allow: true, reader: &fakeToolCallReader{searchErr: visibilityapi.ErrInvalidPromptCursor},
			want: http.StatusBadRequest},
		{name: "the archive failing", allow: true, reader: &fakeToolCallReader{searchErr: errors.New("clickhouse down")},
			want: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp := doGet(t, newPromptsServer(t, &promptGateAuthZ{allowPrompts: tc.allow}, tc.reader), promptSearchPath+"?q=deploy")
			defer resp.Body.Close()
			assert.Equal(t, tc.want, resp.StatusCode)
			if !tc.allow {
				assert.Nil(t, tc.reader.search, "the archive is not read without the grant")
			}
		})
	}
}
