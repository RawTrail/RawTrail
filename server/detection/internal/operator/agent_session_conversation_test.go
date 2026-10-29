package operator

import (
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

const conversationPath = "/api/hosts/h1/agent-sessions/47abb2a1/conversation"

type conversationBody struct {
	Turns []struct {
		PromptID string `json:"prompt_id"`
		Prompt   *struct {
			Text string `json:"text"`
		} `json:"prompt"`
		Commands []struct {
			Command string `json:"command"`
		} `json:"commands"`
	} `json:"turns"`
	TotalMatched  int  `json:"total_matched"`
	PromptsHidden bool `json:"prompts_hidden"`
}

// conversationReader is the commands tests' session with a turn of prompts: the ls and git calls in turn p1, which the person's
// "list and check" began.
func conversationReader() *fakeToolCallReader {
	reader := commandsCalls()
	for i := range reader.calls {
		reader.calls[i].PromptID = "p1"
	}
	reader.prompts = visibilityapi.AgentPromptPage{Prompts: []visibilityapi.AgentPrompt{{PromptID: "p1", Text: "list and check", AtNs: 800}}}
	return reader
}

func getConversation(t *testing.T, srv *httptest.Server, query string) (int, conversationBody, http.Header) {
	t.Helper()
	resp := doGet(t, srv, conversationPath+query)
	defer resp.Body.Close()
	var out conversationBody
	if resp.StatusCode == http.StatusOK {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	}
	return resp.StatusCode, out, resp.Header
}

// spec:server-rest-api/agent-session-conversation/each-prompt-lists-what-the-agent-did-for-it
// spec:server-rest-api/agent-session-conversation/what-belongs-to-no-prompt-is-kept-not-dropped
func TestAgentSessionConversation_Turns(t *testing.T) {
	t.Parallel()
	srv := newPromptsServerTree(t, &promptGateAuthZ{allowPrompts: true}, conversationReader())
	status, got, header := getConversation(t, srv, "")
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, "no-store", header.Get("Cache-Control"))
	require.Len(t, got.Turns, 2)
	assert.Equal(t, "p1", got.Turns[0].PromptID)
	require.NotNil(t, got.Turns[0].Prompt)
	assert.Equal(t, "list and check", got.Turns[0].Prompt.Text)
	assert.Equal(t, []string{"ls", "git status"}, []string{got.Turns[0].Commands[0].Command, got.Turns[0].Commands[1].Command})
	assert.Empty(t, got.Turns[1].PromptID, "the open no call pairs with belongs to no prompt")
	assert.Equal(t, "open -a TextEdit", got.Turns[1].Commands[0].Command)
	assert.False(t, got.PromptsHidden)

	status, found, _ := getConversation(t, srv, "?q=and+CHECK")
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, 1, found.TotalMatched, "the prompt's text is searched")
}

// spec:server-rest-api/agent-session-conversation/without-prompt-read-the-turns-show-without-their-text
func TestAgentSessionConversation_WithoutPromptRead(t *testing.T) {
	t.Parallel()
	srv := newPromptsServerTree(t, &promptGateAuthZ{allowPrompts: false}, conversationReader())
	status, got, _ := getConversation(t, srv, "")
	require.Equal(t, http.StatusOK, status)
	assert.True(t, got.PromptsHidden)
	require.Len(t, got.Turns, 2, "the turns still show")
	require.NotNil(t, got.Turns[0].Prompt)
	assert.Empty(t, got.Turns[0].Prompt.Text, "without the text")

	status, found, _ := getConversation(t, srv, "?q=check")
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, 0, found.TotalMatched, "a search does not reach text the caller cannot read")
}

func TestAgentSessionConversation_Failures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		reader *fakeToolCallReader
		query  string
		want   int
	}{
		{"a malformed cursor", conversationReader(), "?cursor=!!", http.StatusBadRequest},
		{"the prompts read fails", &fakeToolCallReader{promptsErr: errors.New("down")}, "", http.StatusInternalServerError},
		{"the tool-call read fails", &fakeToolCallReader{err: errors.New("down")}, "", http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newPromptsServerTree(t, &promptGateAuthZ{allowPrompts: true}, tc.reader)
			status, _, _ := getConversation(t, srv, tc.query)
			assert.Equal(t, tc.want, status)
		})
	}
}

// newPromptsServerTree is newPromptsServer over the commands tests' session tree.
func newPromptsServerTree(t *testing.T, az identityapi.AuthZ, reader *fakeToolCallReader) *httptest.Server {
	t.Helper()
	h := New(fakeService{buildScopedTree: commandsTree(false)}, az, slog.Default())
	h.SetAgentSessions(summarySession())
	h.SetAgentToolCalls(reader)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}
