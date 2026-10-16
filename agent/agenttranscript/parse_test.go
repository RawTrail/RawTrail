package agenttranscript

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assistantLine is an assistant turn shaped as Claude Code 2.1.296 writes it (probed on edr-dev), trimmed to the fields the parser
// reads and a few it must ignore.
func assistantLine(content string) string {
	return `{"type":"assistant","sessionId":"47abb2a1-d231-4304-8cb1-52f4460fcd11","timestamp":"2026-10-14T08:50:00.512Z",` +
		`"cwd":"/Users/dev/project","isSidechain":false,"uuid":"u1","requestId":"r1","message":{"role":"assistant","content":` +
		content + `}}`
}

// spec:endpoint-event-collection/agent-tool-call-capture/a-bash-tool-call-is-reported-with-its-command
// spec:endpoint-event-collection/agent-tool-call-capture/prompts-and-results-never-leave-the-host
func TestParseLine(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 14, 8, 50, 0, 512_000_000, time.UTC)
	cases := []struct {
		name string
		line string
		want []ToolCall
	}{
		{
			name: "a Bash call",
			line: assistantLine(`[{"type":"tool_use","id":"toolu_01","name":"Bash","input":{"command":"echo gamma > shell.txt",` +
				`"description":"Write gamma"},"caller":{"type":"direct"}}]`),
			want: []ToolCall{{SessionID: "47abb2a1-d231-4304-8cb1-52f4460fcd11", ToolUseID: "toolu_01", ToolName: "Bash",
				Command: "echo gamma > shell.txt", Description: "Write gamma", CWD: "/Users/dev/project", At: at}},
		},
		{
			name: "a file tool names its path, never its contents",
			line: assistantLine(`[{"type":"tool_use","id":"toolu_02","name":"Edit","input":{"file_path":"/r/notes.txt",` +
				`"old_string":"alpha","new_string":"beta","replace_all":false}}]`),
			want: []ToolCall{{SessionID: "47abb2a1-d231-4304-8cb1-52f4460fcd11", ToolUseID: "toolu_02", ToolName: "Edit",
				FilePath: "/r/notes.txt", CWD: "/Users/dev/project", At: at}},
		},
		{
			name: "text beside a call and two calls in one turn",
			line: assistantLine(`[{"type":"text","text":"Running both."},` +
				`{"type":"tool_use","id":"toolu_03","name":"Bash","input":{"command":"ls"}},` +
				`{"type":"tool_use","id":"toolu_04","name":"Bash","input":{"command":"ls"}}]`),
			want: []ToolCall{
				{SessionID: "47abb2a1-d231-4304-8cb1-52f4460fcd11", ToolUseID: "toolu_03", ToolName: "Bash", Command: "ls",
					CWD: "/Users/dev/project", At: at},
				{SessionID: "47abb2a1-d231-4304-8cb1-52f4460fcd11", ToolUseID: "toolu_04", ToolName: "Bash", Command: "ls",
					CWD: "/Users/dev/project", At: at},
			},
		},
		{
			// spec:endpoint-event-collection/agent-tool-call-origin/a-copied-call-names-the-session-it-was-written-under
			name: "a line written under another session id names it, as a fork's copy of its parent's history does",
			line: strings.Replace(assistantLine(`[{"type":"tool_use","id":"toolu_05","name":"Bash","input":{"command":"ls"}}]`),
				`"sessionId"`, `"session_id":"5828cd24-efb7-4b7b-a5bf-f3b24681ba10","sessionId"`, 1),
			want: []ToolCall{{SessionID: "47abb2a1-d231-4304-8cb1-52f4460fcd11", ToolUseID: "toolu_05", ToolName: "Bash", Command: "ls",
				CWD: "/Users/dev/project", OriginSessionID: "5828cd24-efb7-4b7b-a5bf-f3b24681ba10", At: at}},
		},
		{
			name: "a line written under its own session id names none",
			line: strings.Replace(assistantLine(`[{"type":"tool_use","id":"toolu_06","name":"Bash","input":{"command":"ls"}}]`),
				`"sessionId"`, `"session_id":"47abb2a1-d231-4304-8cb1-52f4460fcd11","sessionId"`, 1),
			want: []ToolCall{{SessionID: "47abb2a1-d231-4304-8cb1-52f4460fcd11", ToolUseID: "toolu_06", ToolName: "Bash", Command: "ls",
				CWD: "/Users/dev/project", At: at}},
		},
		{name: "a text-only turn", line: assistantLine(`"Done."`)},
		{name: "a user turn holding a tool result", line: `{"type":"user","sessionId":"s","timestamp":"2026-10-14T08:50:00Z",` +
			`"message":{"content":[{"type":"tool_result","tool_use_id":"toolu_01","content":"secret output"}]}}`},
		{name: "bookkeeping", line: `{"type":"last-prompt","lastPrompt":"do the thing","sessionId":"s"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseLine([]byte(tc.line))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// spec:endpoint-event-collection/agent-tool-call-capture/an-unreadable-line-is-skipped-not-guessed
func TestParseLine_Unreadable(t *testing.T) {
	t.Parallel()
	anyCall := assistantLine(`[{"type":"tool_use","id":"t","name":"Bash","input":{}}]`)
	for name, line := range map[string]string{
		"not JSON":             `{"type":"assistant",`,
		"content not blocks":   assistantLine(`[1,2]`),
		"call without an id":   assistantLine(`[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]`),
		"call without a name":  assistantLine(`[{"type":"tool_use","id":"toolu_05","input":{"command":"ls"}}]`),
		"call without a time":  strings.Replace(anyCall, "2026-10-14T08:50:00.512Z", "", 1),
		"call without session": strings.Replace(anyCall, "47abb2a1-d231-4304-8cb1-52f4460fcd11", "", 1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseLine([]byte(line))
			assert.ErrorIs(t, err, errUnreadable)
		})
	}
}

// spec:endpoint-event-collection/agent-tool-call-capture/a-bash-tool-call-is-reported-with-its-command
func TestToolCall_Payload(t *testing.T) {
	t.Parallel()
	long := "a" + strings.Repeat("é", maxCommandBytes) // two bytes a rune after one: the 8 KiB cut lands inside one
	calls, err := ParseLine([]byte(assistantLine(`[{"type":"tool_use","id":"toolu_06","name":"Bash","input":{"command":"` + long + `"}}]`)))
	require.NoError(t, err)
	require.Len(t, calls, 1)
	p := calls[0].Payload()
	command, ok := p["command"].(string)
	require.True(t, ok)
	assert.LessOrEqual(t, len(command), maxCommandBytes)
	assert.True(t, strings.HasPrefix(long, command))
	assert.True(t, utf8.ValidString(command), "cut on a rune boundary")
	assert.Equal(t, true, p["truncated"])
	assert.Equal(t, map[string]any{"agent_session_id": "s", "tool_use_id": "t", "tool_name": "Read", "file_path": "/r/a", "uid": uint32(501)},
		ToolCall{SessionID: "s", ToolUseID: "t", ToolName: "Read", FilePath: "/r/a", OwnerUID: 501}.Payload(), "empty fields are left out")
}

func FuzzParseLine(f *testing.F) {
	f.Add([]byte(assistantLine(`[{"type":"tool_use","id":"toolu_01","name":"Bash","input":{"command":"ls"}}]`)))
	f.Add([]byte(assistantLine(`"Done."`)))
	f.Add([]byte(`{"type":"user"}`))
	f.Fuzz(func(t *testing.T, line []byte) {
		calls, err := ParseLine(line)
		if err != nil {
			return
		}
		for _, c := range calls {
			if c.SessionID == "" || c.ToolUseID == "" || c.ToolName == "" || len(c.Command) > maxCommandBytes {
				t.Fatalf("accepted an incomplete or oversized call: %+v", c)
			}
		}
	})
}
