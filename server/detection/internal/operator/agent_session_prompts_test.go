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

	identityapi "github.com/rawtrail/rawtrail/server/identity/api"
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

const promptsPath = "/api/hosts/h1/agent-sessions/47abb2a1/prompts"

// promptGateAuthZ allows process read and answers agent_prompt.read as told, recording what each gate asked for.
type promptGateAuthZ struct {
	allowPrompts bool
	asked        []identityapi.Resource
}

func (a *promptGateAuthZ) Allow(_ context.Context, action identityapi.Action, res identityapi.Resource) (identityapi.Decision, error) {
	if action != identityapi.ActionAgentPromptRead {
		return identityapi.Decision{Allow: true, Reason: "granted"}, nil
	}
	a.asked = append(a.asked, res)
	return identityapi.Decision{Allow: a.allowPrompts, Reason: "test"}, nil
}

func newPromptsServer(t *testing.T, az identityapi.AuthZ, reader *fakeToolCallReader) *httptest.Server {
	t.Helper()
	h := New(fakeService{}, az, slog.Default())
	h.SetAgentSessions(summarySession())
	h.SetAgentToolCalls(reader)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// spec:server-rest-api/agent-session-prompts/a-session-s-prompts-page-in-the-order-they-were-typed
// spec:server-rest-api/agent-session-prompts/a-search-keeps-the-prompts-whose-text-holds-it
func TestAgentSessionPrompts_Lists(t *testing.T) {
	t.Parallel()
	reader := &fakeToolCallReader{prompts: visibilityapi.AgentPromptPage{
		Prompts:    []visibilityapi.AgentPrompt{{PromptID: "turn-1", Text: "fix the failing test", Length: 20, AtNs: 100, UID: 501}},
		NextCursor: "c2",
	}}
	az := &promptGateAuthZ{allowPrompts: true}
	srv := newPromptsServer(t, az, reader)

	resp := doGet(t, srv, promptsPath+"?q=+fix+&cursor=c1&limit=10")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var got struct {
		Prompts []struct {
			PromptID string `json:"prompt_id"`
			Text     string `json:"text"`
		} `json:"prompts"`
		NextCursor string `json:"next_cursor"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Len(t, got.Prompts, 1)
	assert.Equal(t, "fix the failing test", got.Prompts[0].Text)
	assert.Equal(t, "c2", got.NextCursor)
	assert.Equal(t, visibilityapi.AgentPromptFilter{HostID: "h1", SessionID: "47abb2a1", Cursor: "c1", Search: "fix", Limit: 10},
		reader.promptFilter, "the session's prompts, from the cursor, by the trimmed search")
	assert.Equal(t, []identityapi.Resource{{Type: "agent_session", ID: "h1/47abb2a1"}}, az.asked, "prompt read is asked for the session")
}

// spec:server-rest-api/agent-session-prompts/prompt-read-is-required-on-top-of-process-read
func TestAgentSessionPrompts_NeedsPromptRead(t *testing.T) {
	t.Parallel()
	reader := &fakeToolCallReader{}
	srv := newPromptsServer(t, &promptGateAuthZ{allowPrompts: false}, reader)
	resp := doGet(t, srv, promptsPath)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, visibilityapi.AgentPromptFilter{}, reader.promptFilter, "no prompt is read")
}

func TestAgentSessionPrompts_Failures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"a malformed cursor", visibilityapi.ErrInvalidPromptCursor, http.StatusBadRequest},
		{"a failed read", errors.New("clickhouse down"), http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newPromptsServer(t, &promptGateAuthZ{allowPrompts: true}, &fakeToolCallReader{promptsErr: tc.err})
			resp := doGet(t, srv, promptsPath+"?cursor=x")
			defer resp.Body.Close()
			assert.Equal(t, tc.want, resp.StatusCode)
		})
	}
}
