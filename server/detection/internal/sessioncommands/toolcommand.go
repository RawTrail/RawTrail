// Package sessioncommands shapes an AI agent session's processes into the commands it ran (issue #1423): one group per command,
// labelled with the command the agent ran, paired with the tool call the agent reported for it. The session view's Commands lists
// these groups a page at a time.
package sessioncommands

import (
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Reading the command an AI agent ran out of the shell it ran it in (issue #1284).
//
// Claude Code runs each Bash tool call as one `<shell> -c '<script>'` whose script restores the user's shell snapshot, runs the
// model's command through `eval`, and records the working directory afterwards:
//
//	source <snapshot> 2>/dev/null || true && ... && eval '<command>' < /dev/null && pwd -P >| /tmp/claude-XXXX-cwd
//
// The command is the eval's argument, one shell word written as single-quoted runs with any embedded quote escaped, as '\'' or as
// Claude Code's '"'"' (close the quote, a double-quoted quote, reopen it), which the shell joins back into one word (issue #1350).

var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "fish": true}

const evalPrefix = "eval "

// IsShellPath reports whether an executable is one of the common shells, by its basename.
func IsShellPath(p string) bool { return shells[path.Base(p)] }

// shellScript returns the script of a `<shell> -c <script>` argv.
func shellScript(args []string) (string, bool) {
	if len(args) != 3 || args[1] != "-c" || !IsShellPath(args[0]) {
		return "", false
	}
	return args[2], true
}

// doubleQuotedEscapes are the characters a backslash escapes inside double quotes; before any other, the backslash is literal.
const doubleQuotedEscapes = "$`\"\\\n"

// readDoubleQuoted reads the double-quoted run whose opening quote is at open: its text, with the backslash escapes POSIX applies
// inside double quotes, a backslash-newline being a line continuation that leaves nothing, and the index past its closing quote. It
// reports false when the quote is never closed.
func readDoubleQuoted(text string, open int) (string, int, bool) {
	var run strings.Builder
	for i := open + 1; i < len(text); {
		c := text[i]
		switch {
		case c == '"':
			return run.String(), i + 1, true
		case c == '\\' && i+1 < len(text) && strings.IndexByte(doubleQuotedEscapes, text[i+1]) >= 0:
			if text[i+1] != '\n' {
				run.WriteByte(text[i+1])
			}
			i += 2
		default:
			run.WriteByte(c)
			i++
		}
	}
	return "", 0, false
}

// spaceAt reports whether the character at i is whitespace, the boundary of a shell word.
func spaceAt(text string, i int) bool {
	r, _ := utf8.DecodeRuneInString(text[i:])
	return unicode.IsSpace(r)
}

// readShellWord reads the POSIX shell word starting at start: single-quoted runs taken literally, double-quoted runs with their
// escapes, a backslash escaping the next character, and bare characters, joined up to unquoted whitespace or the end. It reports
// false when a quote is never closed.
func readShellWord(text string, start int) (string, bool) {
	var word strings.Builder
	i := start
	for i < len(text) && !spaceAt(text, i) {
		switch text[i] {
		case '\'':
			closing := strings.IndexByte(text[i+1:], '\'')
			if closing < 0 {
				return "", false
			}
			word.WriteString(text[i+1 : i+1+closing])
			i += closing + 2
		case '"':
			run, end, ok := readDoubleQuoted(text, i)
			if !ok {
				return "", false
			}
			word.WriteString(run)
			i = end
		case '\\':
			if i+1 >= len(text) {
				word.WriteByte('\\')
				i++
				continue
			}
			// A backslash-newline is a line continuation, which leaves nothing.
			if text[i+1] != '\n' {
				word.WriteByte(text[i+1])
			}
			i += 2
		default:
			word.WriteByte(text[i])
			i++
		}
	}
	return word.String(), true
}

// ToolCommand returns the command an agent's tool-call shell ran: the eval argument of its wrapper script. It reports false when
// the argv is not such a wrapper, so the caller falls back to the argv itself.
func ToolCommand(args []string) (string, bool) {
	script, ok := shellScript(args)
	if !ok {
		return "", false
	}
	at, ok := lastTopLevelEval(script)
	if !ok {
		return "", false
	}
	return readShellWord(script, at+len(evalPrefix))
}

// lastTopLevelEval returns where the script's last eval word starts, skipping quoted runs, single and double: the model's command can
// itself contain "eval", inside the quotes the wrapper put around it, and that one is not the wrapper's. It reports false when the
// script has no eval.
func lastTopLevelEval(script string) (int, bool) {
	last, found := 0, false
	for i := 0; i < len(script); {
		switch script[i] {
		case '\'':
			closing := strings.IndexByte(script[i+1:], '\'')
			if closing < 0 {
				return last, found
			}
			i += closing + 2
		case '"':
			_, end, ok := readDoubleQuoted(script, i)
			if !ok {
				return last, found
			}
			i = end
		case '\\':
			i += 2
		default:
			// A word boundary, so a word that merely ends in "eval" (retrieval) is not taken for the command.
			if strings.HasPrefix(script[i:], evalPrefix) && (i == 0 || evalBoundary(script, i)) {
				last, found = i, true
			}
			i++
		}
	}
	return last, found
}

// evalBoundary reports whether the character before i ends a word: whitespace or a shell operator.
func evalBoundary(script string, i int) bool {
	r, _ := utf8.DecodeLastRuneInString(script[:i])
	return unicode.IsSpace(r) || strings.ContainsRune(";&|({", r)
}

// CommandLine renders a process's argv as one line, or its path when it carries no argv (a fork that never exec'd).
func CommandLine(p string, args []string) string {
	if len(args) > 0 {
		return strings.Join(args, " ")
	}
	return p
}
