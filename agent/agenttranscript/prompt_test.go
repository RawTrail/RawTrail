package agenttranscript

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// userLine is a user turn shaped as Claude Code 2.1.309 writes it, trimmed to the fields the parser reads and a few it must ignore.
// extra is spliced in after the type: an origin, a prompt source, isMeta.
func userLine(content, extra string) string {
	return `{"parentUuid":null,"isSidechain":false,"type":"user"` + extra + `,"uuid":"e1f0c2a4-1","promptId":"p-1",` +
		`"sessionId":"47abb2a1-d231-4304-8cb1-52f4460fcd11","timestamp":"2026-10-28T09:30:00.250Z","cwd":"/Users/dev/project",` +
		`"version":"2.1.309","userType":"external","message":{"role":"user","content":` + content + `}}`
}

const typed = `,"origin":{"kind":"human"},"promptSource":"typed"`

// spec:endpoint-event-collection/agent-prompt-capture/a-typed-prompt-is-reported-with-its-turn
func TestParseLine_Prompt(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 28, 9, 30, 0, 250_000_000, time.UTC)
	prompt := func(text, source string) *Prompt {
		return &Prompt{SessionID: "47abb2a1-d231-4304-8cb1-52f4460fcd11", EntryID: "e1f0c2a4-1", PromptID: "p-1", Text: text,
			Length: len(text), Source: source, CWD: "/Users/dev/project", At: at}
	}
	for _, tc := range []struct {
		name string
		line string
		want *Prompt
	}{
		{"a typed prompt", userLine(`"fix the failing auth test"`, typed), prompt("fix the failing auth test", "typed")},
		{"a prompt queued while the agent worked", userLine(`"also run the linter"`, `,"origin":{"kind":"human"},"promptSource":"queued"`),
			prompt("also run the linter", "queued")},
		{"a slash command reads as the user typed it", userLine(`"<command-message>ai-review-fixes</command-message>\n`+
			`<command-name>/ai-review-fixes</command-name>\n<command-args>PR #1438</command-args>"`, typed),
			&Prompt{SessionID: "47abb2a1-d231-4304-8cb1-52f4460fcd11", EntryID: "e1f0c2a4-1", PromptID: "p-1",
				Text: "/ai-review-fixes PR #1438", Length: len("<command-message>ai-review-fixes</command-message>\n" +
					"<command-name>/ai-review-fixes</command-name>\n<command-args>PR #1438</command-args>"), Source: "typed",
				CWD: "/Users/dev/project", At: at}},
		// spec:endpoint-event-collection/agent-prompt-capture/a-prompt-passed-on-the-command-line-is-reported
		{"a prompt passed on the command line, which records no origin", userLine(`"write the release notes"`, `,"promptSource":"sdk"`),
			prompt("write the release notes", "sdk")},
		{"a prompt that mentions the command tags is kept as typed",
			userLine(`"Explain <command-name>/deploy</command-name> before using it"`, typed),
			prompt("Explain <command-name>/deploy</command-name> before using it", "typed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseLine([]byte(tc.line))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.Prompt)
			assert.Empty(t, got.Calls)
		})
	}
}

// What a person did not type is not a prompt: the agent's own turns, its notifications, a compacted history, a tool's result, and a
// turn from an agent too old to record who it came from.
//
// spec:endpoint-event-collection/agent-prompt-capture/only-what-a-person-typed-is-a-prompt
func TestParseLine_NotAPrompt(t *testing.T) {
	t.Parallel()
	for name, line := range map[string]string{
		"a skill's instructions the agent wrote": userLine(`"Base directory for this skill: /x"`, typed+`,"isMeta":true`),
		"a background task's notification":       userLine(`"<task-notification>done</task-notification>"`, `,"origin":{"kind":"task-notification"},"promptSource":"system"`),
		"a peer session's message":               userLine(`"hello from the other lane"`, `,"origin":{"kind":"peer"},"promptSource":"system","isMeta":true`),
		"a compacted history":                    userLine(`"This session is being continued from a previous conversation"`, `,"isCompactSummary":true`),
		"a compacted history of a claude -p run": userLine(`"This session is being continued"`, `,"promptSource":"sdk","isCompactSummary":true`),
		"a claude -p run's own instructions":     userLine(`"Caveat: the messages below were generated"`, `,"promptSource":"sdk","isMeta":true`),
		"a /clear, which records no source":      userLine(`"<command-name>/clear</command-name>"`, ""),
		"a source of sdk beside another origin":  userLine(`"done"`, `,"origin":{"kind":"task-notification"},"promptSource":"sdk"`),
		"a subagent's instructions":              userLine(`"Search the repo for the config loader"`, typed+`,"isSidechain":true`),
		"an older agent that records no origin":  userLine(`"fix it"`, ""),
		"a tool's result":                        userLine(`[{"tool_use_id":"toolu_1","type":"tool_result","content":"secret output"}]`, typed),
		"a turn of blocks a person did not type": userLine(`[{"type":"text","text":"[Request interrupted by user]"}]`, typed),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseLine([]byte(line))
			require.NoError(t, err)
			assert.Nil(t, got.Prompt)
		})
	}
}

// spec:endpoint-event-collection/agent-prompt-capture/a-prompt-is-redacted-and-bounded-before-it-leaves
func TestParseLine_PromptRedactedAndBounded(t *testing.T) {
	t.Parallel()
	key := "AKIA" + "IOSFODNN7EXAMPLE"
	got, err := ParseLine([]byte(userLine(`"deploy with `+key+` please"`, typed)))
	require.NoError(t, err)
	require.NotNil(t, got.Prompt)
	assert.Equal(t, "deploy with [REDACTED:aws-access-key] please", got.Prompt.Text)
	assert.Equal(t, 1, got.Prompt.Redacted)
	assert.Equal(t, len("deploy with "+key+" please"), got.Prompt.Length, "the length is the prompt's as typed")

	long := "a" + strings.Repeat("é", maxPromptBytes) // the cut lands inside a two-byte rune
	got, err = ParseLine([]byte(userLine(`"`+long+`"`, typed)))
	require.NoError(t, err)
	require.NotNil(t, got.Prompt)
	assert.True(t, got.Prompt.Truncated)
	assert.LessOrEqual(t, len(got.Prompt.Text), maxPromptBytes)
	assert.True(t, utf8.ValidString(got.Prompt.Text), "cut on a rune boundary")
	assert.True(t, strings.HasPrefix(long, got.Prompt.Text))
	assert.Equal(t, len(long), got.Prompt.Length)
}

// spec:endpoint-event-collection/agent-tool-call-reporting/an-unreadable-line-is-skipped-not-guessed
func TestParseLine_PromptUnreadable(t *testing.T) {
	t.Parallel()
	for name, line := range map[string]string{
		"no entry id":          strings.Replace(userLine(`"x"`, typed), `"uuid":"e1f0c2a4-1",`, "", 1),
		"no session":           strings.Replace(userLine(`"x"`, typed), "47abb2a1-d231-4304-8cb1-52f4460fcd11", "", 1),
		"no time":              strings.Replace(userLine(`"x"`, typed), "2026-10-28T09:30:00.250Z", "", 1),
		"content not a string": userLine(`"x`, typed),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseLine([]byte(line))
			assert.ErrorIs(t, err, errUnreadable)
		})
	}
}

// A user entry names the turn it belongs to: the prompt that began it, and each tool result in it; an assistant entry names none.
//
// spec:endpoint-event-collection/agent-prompt-capture/a-typed-prompt-is-reported-with-its-turn
func TestParseLine_Turn(t *testing.T) {
	t.Parallel()
	got, err := ParseLine([]byte(userLine(`"fix it"`, typed)))
	require.NoError(t, err)
	assert.Equal(t, "p-1", got.Turn)
	got, err = ParseLine([]byte(userLine(`[{"tool_use_id":"toolu_1","type":"tool_result","content":"ok"}]`, "")))
	require.NoError(t, err)
	assert.Equal(t, "p-1", got.Turn, "a tool result belongs to its turn")
	got, err = ParseLine([]byte(assistantLine(`[{"type":"tool_use","id":"toolu_01","name":"Bash","input":{"command":"ls"}}]`)))
	require.NoError(t, err)
	assert.Empty(t, got.Turn)
	assert.Empty(t, got.Calls[0].PromptID, "the parser leaves a call's turn to the reader")
}

// The same prompt read again is the same event, and the owner is part of it, as for a tool call.
func TestPrompt_EventID(t *testing.T) {
	t.Parallel()
	p := Prompt{SessionID: "s", EntryID: "e", OwnerUID: 501}
	assert.Equal(t, p.EventID(), Prompt{SessionID: "s", EntryID: "e", OwnerUID: 501, Text: "edited"}.EventID())
	assert.NotEqual(t, p.EventID(), Prompt{SessionID: "s", EntryID: "e", OwnerUID: 502}.EventID())
	assert.NotEqual(t, p.EventID(), ToolCall{SessionID: "s", ToolUseID: "e", OwnerUID: 501}.EventID(), "a prompt is not a tool call")
}

// headlessLine is a user entry of a claude -p session as Claude Code 2.1.321 writes it (issue #1543), trimmed to the fields the
// reader uses: its own id, the entry it follows, and its content. extra is spliced in after the type.
func headlessLine(uuid, parent, content, extra string) string {
	return `{"parentUuid":` + parent + `,"isSidechain":false,"type":"user"` + extra + `,"uuid":"` + uuid + `","promptId":"p-9",` +
		`"sessionId":"47abb2a1-d231-4304-8cb1-52f4460fcd11","timestamp":"2026-11-09T22:43:00.250Z","cwd":"/Users/dev/project",` +
		`"version":"2.1.321","entrypoint":"sdk-cli","userType":"external","message":{"role":"user","content":` + content + `}}` + "\n"
}

// caveatLine is the meta entry Claude Code writes just before it runs a slash command as a local command.
func caveatLine(uuid, entrypoint string) string {
	return strings.Replace(headlessLine(uuid, `"q-1"`, `"<local-command-caveat>Caveat: The messages below were generated by the `+
		`user while running local commands. DO NOT respond to these messages.</local-command-caveat>"`, `,"isMeta":true`),
		`"entrypoint":"sdk-cli"`, `"entrypoint":"`+entrypoint+`"`, 1)
}

// spec:endpoint-event-collection/agent-prompt-capture/a-slash-command-passed-on-the-command-line-is-reported
//
// A slash command passed to claude -p is recorded as Claude Code records a local command: a meta caveat, then the command as plain
// text with neither an origin nor a prompt source. It is the person's prompt only right after its caveat, in a headless session.
func TestReader_HeadlessSlashCommandIsAPrompt(t *testing.T) {
	t.Parallel()
	command := `"/code-review the empty repo"`
	for _, tc := range []struct {
		name  string
		lines string
		want  []string
	}{
		{"a command passed to claude -p, after its caveat",
			caveatLine("cav-1", "sdk-cli") + headlessLine("cmd-1", `"cav-1"`, command, ""), []string{"/code-review the empty repo"}},
		{"the same text without a caveat before it",
			headlessLine("cmd-1", `"q-1"`, command, ""), nil},
		{"the same text after another entry than the caveat",
			caveatLine("cav-1", "sdk-cli") + headlessLine("cmd-1", `"other-entry"`, command, ""), nil},
		{"text after a caveat that is not a slash command",
			caveatLine("cav-1", "sdk-cli") + headlessLine("cmd-1", `"cav-1"`, `"summarize the repo"`, ""), nil},
		{"a command after another meta entry the agent wrote, such as a skill's instructions",
			headlessLine("skill-1", `"q-1"`, `"Base directory for this skill: /Users/dev/.claude/skills/review"`, `,"isMeta":true`) +
				headlessLine("cmd-1", `"skill-1"`, command, ""), nil},
		{"a line the agent wrote itself, which records its own origin",
			caveatLine("cav-1", "sdk-cli") + headlessLine("cmd-1", `"cav-1"`, command, `,"origin":{"kind":"task-notification"}`), nil},
		{"an interactive local command such as /clear, which records no origin",
			caveatLine("cav-1", "cli") + strings.Replace(headlessLine("cmd-1", `"cav-1"`,
				`"<command-name>/clear</command-name>\n<command-message>clear</command-message>\n<command-args></command-args>"`, ""),
				`"entrypoint":"sdk-cli"`, `"entrypoint":"cli"`, 1), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			q := &recordingQueue{}
			r, path := newTestReader(t, q, "H1")
			appendTo(t, path, tc.lines)
			r.RunOnce(t.Context())
			var got []string
			for _, e := range q.events {
				if e["event_type"] == PromptEventType {
					payload, _ := e["payload"].(map[string]any)
					got = append(got, payload["text"].(string))
					assert.Equal(t, sdkPromptSource, payload["source"], "a command passed on the command line reads as one")
					assert.Equal(t, "p-9", payload["prompt_id"], "with its own turn, which its work carries too")
				}
			}
			assert.Equal(t, tc.want, got)
		})
	}
}
