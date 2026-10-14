// Reading the command an AI agent ran out of the shell it ran it in (issue #1284).
//
// Claude Code runs each Bash tool call as one `<shell> -c '<script>'` whose script restores the user's shell snapshot, runs the
// model's command through `eval`, and records the working directory afterwards:
//
//   source <snapshot> 2>/dev/null || true && ... && eval '<command>' < /dev/null && pwd -P >| /tmp/claude-XXXX-cwd
//
// The command is the eval's argument, one shell word written as single-quoted runs with any embedded quote escaped as '\''.

import { basename } from "../Search/format";

const SHELLS = new Set(["sh", "bash", "zsh", "dash", "ksh", "fish"]);
const EVAL_PREFIX = "eval ";
// SHELL_C_ARGC is the length of a `<shell> -c <script>` argv.
const SHELL_C_ARGC = 3;

// isShellPath reports whether an executable is one of the common shells, by its basename.
export function isShellPath(path: string): boolean {
  return SHELLS.has(basename(path));
}

// shellScript returns the script of a `<shell> -c <script>` argv, or null for any other argv.
export function shellScript(args: readonly string[] | undefined): string | null {
  if (args?.length !== SHELL_C_ARGC) return null;
  const [shell, flag, script] = args;
  return flag === "-c" && isShellPath(shell) ? script : null;
}

// readShellWord reads the POSIX shell word starting at start: single-quoted runs taken literally, a backslash escaping the next
// character, and bare characters, up to unquoted whitespace or the end. Null when a single quote is never closed.
export function readShellWord(text: string, start: number): string | null {
  let word = "";
  let i = start;
  while (i < text.length && !/\s/.test(text.charAt(i))) {
    if (text.charAt(i) === "'") {
      const close = text.indexOf("'", i + 1);
      if (close < 0) return null;
      word += text.slice(i + 1, close);
      i = close + 1;
    } else if (text.charAt(i) === "\\" && i + 1 < text.length) {
      word += text.charAt(i + 1);
      i += 2;
    } else {
      word += text.charAt(i);
      i += 1;
    }
  }
  return word;
}

// toolCommand returns the command an agent's tool-call shell ran: the eval argument of its wrapper script. Null when the argv is not
// such a wrapper, so the caller falls back to the argv itself.
export function toolCommand(args: readonly string[] | undefined): string | null {
  const script = shellScript(args);
  if (script === null) return null;
  const at = lastTopLevelEval(script);
  return at === null ? null : readShellWord(script, at + EVAL_PREFIX.length);
}

// lastTopLevelEval returns where the script's last eval word starts, skipping quoted runs: the model's command can itself contain
// "eval", inside the quotes the wrapper put around it, and that one is not the wrapper's. Null when the script has no eval.
function lastTopLevelEval(script: string): number | null {
  let last: number | null = null;
  let i = 0;
  while (i < script.length) {
    if (script.charAt(i) === "'") {
      const close = script.indexOf("'", i + 1);
      if (close < 0) return last;
      i = close + 1;
    } else if (script.charAt(i) === "\\") {
      i += 2;
    } else {
      // A word boundary, so a word that merely ends in "eval" (retrieval) is not taken for the command.
      if (script.startsWith(EVAL_PREFIX, i) && (i === 0 || /[\s;&|({]/.test(script.charAt(i - 1)))) last = i;
      i += 1;
    }
  }
  return last;
}

// commandLine renders a process's argv as one line, or its path when it carries no argv (a fork that never exec'd).
export function commandLine(path: string, args: readonly string[] | undefined): string {
  return args && args.length > 0 ? args.join(" ") : path;
}
