package agenttranscript

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// usageLine is one transcript entry of an assistant message, as Claude Code writes one per content block of a streamed message, each
// with the whole message's usage. extra is spliced in after the type: an origin session for a copied line, isSidechain.
func usageLine(messageID, model, extra string) string {
	return `{"type":"assistant"` + extra + `,"sessionId":"47abb2a1-d231-4304-8cb1-52f4460fcd11","timestamp":"2026-10-28T09:31:00.500Z",` +
		`"cwd":"/Users/dev/project","isSidechain":false,"uuid":"a-` + messageID + `","message":{"id":"` + messageID + `","model":"` +
		model + `","role":"assistant","content":[{"type":"text","text":"done"}],"usage":{"input_tokens":3,"output_tokens":120,` +
		`"cache_creation_input_tokens":512,"cache_read_input_tokens":4096,"output_tokens_details":{"thinking_tokens":40}}}}`
}

func TestParseLine_Usage(t *testing.T) {
	t.Parallel()
	got, err := ParseLine([]byte(usageLine("msg_01", "claude-opus-5-5", "")))
	require.NoError(t, err)
	require.NotNil(t, got.Usage)
	assert.Equal(t, Usage{SessionID: "47abb2a1-d231-4304-8cb1-52f4460fcd11", MessageID: "msg_01", Model: "claude-opus-5-5", Input: 3,
		Output: 120, CacheCreation: 512, CacheRead: 4096, Thinking: 40, At: time.Date(2026, 10, 28, 9, 31, 0, 500_000_000, time.UTC)},
		*got.Usage)

	// spec:endpoint-event-collection/agent-token-usage-reporting/a-message-claude-code-wrote-itself-is-not-counted
	got, err = ParseLine([]byte(usageLine("msg_02", "<synthetic>", "")))
	require.NoError(t, err)
	assert.Nil(t, got.Usage, "a message Claude Code wrote itself used no tokens")

	// A line naming another session is a fork's copy, or a message of a session /clear started: sent with that session named, for the
	// server to tell apart.
	// spec:endpoint-event-collection/agent-token-usage-reporting/a-line-naming-another-session-is-sent-naming-it
	got, err = ParseLine([]byte(usageLine("msg_03", "claude-opus-5-5", `,"session_id":"5828cd24-efb7-4b7b-a5bf-f3b24681ba10"`)))
	require.NoError(t, err)
	require.NotNil(t, got.Usage)
	assert.Equal(t, "5828cd24-efb7-4b7b-a5bf-f3b24681ba10", got.Usage.OriginSessionID)

	_, err = ParseLine([]byte(usageLine("", "claude-opus-5-5", "")))
	assert.ErrorIs(t, err, errUnreadable, "a message without its id cannot be counted once")
}

func TestParseLine_PermissionMode(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ line, want string }{
		"a permission-mode entry": {
			`{"type":"permission-mode","permissionMode":"bypassPermissions","sessionId":"47abb2a1-d231-4304-8cb1-52f4460fcd11"}`,
			"bypassPermissions"},
		"a user entry":             {userLine(`"run it"`, typed+`,"permissionMode":"plan"`), "plan"},
		"an entry that names none": {userLine(`"run it"`, typed), ""},
	} {
		got, err := ParseLine([]byte(tc.line))
		require.NoError(t, err, name)
		assert.Equal(t, tc.want, got.Mode, name)
		if tc.want != "" {
			assert.Equal(t, "47abb2a1-d231-4304-8cb1-52f4460fcd11", got.ModeSession, name)
		}
	}
}

type wireUsage struct {
	SessionID      string `json:"agent_session_id"`
	MessageID      string `json:"message_id"`
	Model          string `json:"model"`
	Input          int64  `json:"input_tokens"`
	Output         int64  `json:"output_tokens"`
	CacheCreation  int64  `json:"cache_creation_input_tokens"`
	CacheRead      int64  `json:"cache_read_input_tokens"`
	Thinking       int64  `json:"thinking_tokens"`
	PromptID       string `json:"prompt_id"`
	PermissionMode string `json:"permission_mode"`
	Sidechain      bool   `json:"sidechain"`
	Origin         string `json:"origin_session_id"`
	ForkedFrom     string `json:"forked_from_session_id"`
	UID            uint32 `json:"uid"`
}

// A message's usage survives the wire whole, and an optional field left empty is absent rather than sent empty.
func TestUsagePayloadRoundTrip(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		tokens := rapid.Int64Range(0, 1<<40)
		u := Usage{
			SessionID: rapid.StringN(1, 64, -1).Draw(t, "session"), MessageID: rapid.StringN(1, 64, -1).Draw(t, "message"),
			Model: rapid.String().Draw(t, "model"), Input: tokens.Draw(t, "input"), Output: tokens.Draw(t, "output"),
			CacheCreation: tokens.Draw(t, "cacheCreation"), CacheRead: tokens.Draw(t, "cacheRead"), Thinking: tokens.Draw(t, "thinking"),
			PromptID: rapid.String().Draw(t, "prompt"), PermissionMode: rapid.String().Draw(t, "mode"),
			Sidechain: rapid.Bool().Draw(t, "sidechain"), OwnerUID: rapid.Uint32().Draw(t, "uid"),
			OriginSessionID: rapid.String().Draw(t, "origin"), ForkedFromSessionID: rapid.String().Draw(t, "forkedFrom"),
		}
		payload := u.Payload()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got wireUsage
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		want := wireUsage{u.SessionID, u.MessageID, u.Model, u.Input, u.Output, u.CacheCreation, u.CacheRead, u.Thinking, u.PromptID,
			u.PermissionMode, u.Sidechain, u.OriginSessionID, u.ForkedFromSessionID, u.OwnerUID}
		if got != want {
			t.Fatalf("round trip changed the usage: got %+v, want %+v", got, want)
		}
		for key, present := range map[string]bool{"prompt_id": u.PromptID != "", "permission_mode": u.PermissionMode != "",
			"sidechain": u.Sidechain, "origin_session_id": u.OriginSessionID != "", "forked_from_session_id": u.ForkedFromSessionID != ""} {
			if _, ok := payload[key]; ok != present {
				t.Fatalf("%s present=%v, want %v", key, ok, present)
			}
		}
	})
}
