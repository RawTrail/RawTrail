package sessioncommands

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// wrap builds the script Claude Code 2.1.296 runs a Bash tool call in, verbatim from an edr-dev capture apart from the command.
func wrap(quotedCommand string) []string {
	script := "source /Users/victor/.claude/shell-snapshots/snapshot-zsh-1791902227385-pju9os.sh 2>/dev/null || true && " +
		"setopt NO_EXTENDED_GLOB NO_BARE_GLOB_QUAL 2>/dev/null || true && " +
		`{ \builtin unalias -- 'unsetenv'; ` + // dash-lint:ignore (the shell's end-of-options, verbatim)
		`\builtin unset -f -- 'unsetenv'; } >/dev/null 2>&1 || true && ` + // dash-lint:ignore (the same)
		"eval " + quotedCommand + " < /dev/null && pwd -P >| /tmp/claude-786b-cwd"
	return []string{"/bin/zsh", "-c", script}
}

// spec:server-rest-api/agent-session-commands/commands-are-labelled-and-paired-with-their-tool-calls
func TestToolCommand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, quoted, want string }{
		// Each command an edr-dev Claude Code session ran, as the wrapper quoted it.
		{"a pipeline", `'ls /private/tmp | head -3'`, "ls /private/tmp | head -3"},
		{"a backgrounded subshell", `'(sleep 47 &)'`, "(sleep 47 &)"},
		{"nohup with redirections", `'nohup sleep 48 >/dev/null 2>&1 &'`, "nohup sleep 48 >/dev/null 2>&1 &"},
		{"env -i", `'env -i /bin/echo qa1280'`, "env -i /bin/echo qa1280"},
		{"double quotes inside", `'/bin/sh -c "sleep 1; echo qa1280-done"'`, `/bin/sh -c "sleep 1; echo qa1280-done"`},
		// A single quote in the command closes the run, escapes a quote, and reopens it.
		{"an embedded single quote", `'echo '\''it'\''s'\'''`, "echo 'it's'"},
		// The wrapper always opens a run, so a command starting with a quote begins with an empty one.
		{"a quote at the very start", `''\''quoted'\'' rest'`, "'quoted' rest"},
		{"an empty command", `''`, ""},
		// Claude Code 2.1.300 and later escape an embedded quote as '"'"': close, a double-quoted quote, reopen (issue #1350).
		{
			"the double-quoted escape, as edr-dev captured it",
			`'open -b com.apple.TextEdit && sleep 5 && osascript -e '"'"'quit app "TextEdit"'"'"''`,
			`open -b com.apple.TextEdit && sleep 5 && osascript -e 'quit app "TextEdit"'`,
		},
		{"the double-quoted escape at the start", `''"'"'quoted'"'"' rest'`, "'quoted' rest"},
		{"the double-quoted escape in the middle", `'echo it'"'"'s'`, "echo it's"},
		{"the double-quoted escape at the end", `'echo '"'"''`, "echo '"},
		// The RSA rerun's first call: double quotes, a newline and the escape in one command.
		{
			"a multi-line command with the double-quoted escape",
			"'npm init -y >/dev/null && echo \"const esbuild = require('\"'\"'esbuild'\"'\"');\nconsole.log(esbuild.version);\" > build.js'",
			"npm init -y >/dev/null && echo \"const esbuild = require('esbuild');\nconsole.log(esbuild.version);\" > build.js",
		},
		{"the wrapper's eval when the command itself runs eval", `'cd /tmp && eval '\''echo hi'\'''`, "cd /tmp && eval 'echo hi'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ToolCommand(wrap(tc.quoted))
			assert.True(t, ok)
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("the last eval, the model's, past one in the restored snapshot", func(t *testing.T) {
		t.Parallel()
		got, ok := ToolCommand([]string{"/bin/bash", "-c", "eval 'snapshot setup' && eval 'git status' < /dev/null && pwd -P >| /tmp/c"})
		assert.True(t, ok)
		assert.Equal(t, "git status", got)
	})
	t.Run("an escaped quote before the eval is not a quoted run", func(t *testing.T) {
		t.Parallel()
		got, ok := ToolCommand([]string{"/bin/zsh", "-c", `echo \' && eval 'git status' < /dev/null`})
		assert.True(t, ok)
		assert.Equal(t, "git status", got)
	})
	t.Run("an eval inside a double-quoted run is not the wrapper's", func(t *testing.T) {
		t.Parallel()
		got, ok := ToolCommand([]string{"/bin/zsh", "-c", `eval 'git status' && echo "then eval 'x'"`})
		assert.True(t, ok)
		assert.Equal(t, "git status", got)
	})
}

func TestToolCommand_NotAWrapper(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"no argv", nil},
		{"a plain exec", []string{"git", "status"}},
		{"a shell without -c", []string{"/bin/zsh", "-l"}},
		{"a shell with another flag", []string{"/bin/zsh", "-l", "eval 'git status'"}},
		{"a non-shell -c", []string{"/usr/bin/python3", "-c", "eval 'x'"}},
		{"a shell script with no eval", []string{"/bin/zsh", "-c", "git status"}},
		{"an unterminated quote", []string{"/bin/zsh", "-c", "eval 'git status"}},
		{"an unterminated double quote", []string{"/bin/zsh", "-c", `eval 'git'"status`}},
		{"a word that only ends in eval", []string{"/bin/zsh", "-c", "echo retrieval payload"}},
		{"extra arguments", []string{"/bin/zsh", "-c", "eval 'x'", "extra"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, ok := ToolCommand(tc.args)
			assert.False(t, ok)
		})
	}
}

func TestReadShellWord(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, text, want string }{
		{"stops at unquoted whitespace, keeps quoted", `'a b'c d`, "a bc"},
		{"an escaped space", `x\ y z`, "x y"},
		{"only POSIX escapes inside double quotes", `"a\$b\"c\\d\e"`, `a$b"c\d\e`},
		{"a line continuation inside double quotes", "\"echo a\\\nb\"", "echo ab"},
		{"a line continuation outside quotes", "echo\\\nb", "echob"},
		{"a bare newline inside double quotes", "\"echo a\nb\"", "echo a\nb"},
		{"a trailing backslash with nothing to escape", `ab\`, `ab\`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := readShellWord(tc.text, 0)
			assert.True(t, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestShellPathsAndCommandLine(t *testing.T) {
	t.Parallel()
	assert.True(t, IsShellPath("/opt/homebrew/bin/bash"))
	assert.True(t, IsShellPath("sh"))
	assert.False(t, IsShellPath("/usr/bin/python3"))
	assert.Equal(t, "ls -la", CommandLine("/bin/ls", []string{"ls", "-la"}))
	assert.Equal(t, "/bin/zsh", CommandLine("/bin/zsh", nil))
	assert.Equal(t, "/bin/zsh", CommandLine("/bin/zsh", []string{}))
}

// quoteLikeClaudeCode quotes a command as Claude Code 2.1.300 and later write it into the wrapper: one single-quoted run, each embedded
// quote written as '"'"'.
func quoteLikeClaudeCode(command string) string {
	return "'" + strings.ReplaceAll(command, "'", `'"'"'`) + "'"
}

// Any command the agent runs reads back as it ran, whatever quotes, evals, newlines or operators it holds.
func TestToolCommand_RoundTripProperty(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		command := rapid.StringOfN(rapid.SampledFrom([]rune("ab \t\n'\"\\$`;&|(){}eval")), 0, 40, -1).Draw(t, "command")
		got, ok := ToolCommand(wrap(quoteLikeClaudeCode(command)))
		if !ok || got != command {
			t.Fatalf("ToolCommand read %q (ok=%v), want %q", got, ok, command)
		}
	})
}
