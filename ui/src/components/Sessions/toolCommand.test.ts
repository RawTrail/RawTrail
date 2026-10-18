import { describe, it, expect } from "vitest";

import { commandLine, readShellWord, shellScript, toolCommand } from "./toolCommand";

// wrap builds the script Claude Code 2.1.296 runs a Bash tool call in, verbatim from an edr-dev capture apart from the command.
function wrap(quotedCommand: string): string[] {
  const script =
    "source /Users/victor/.claude/shell-snapshots/snapshot-zsh-1791902227385-pju9os.sh 2>/dev/null || true && " +
    "setopt NO_EXTENDED_GLOB NO_BARE_GLOB_QUAL 2>/dev/null || true && " +
    "{ \\builtin unalias -- 'unsetenv'; \\builtin unset -f -- 'unsetenv'; } >/dev/null 2>&1 || true && " +
    `eval ${quotedCommand} < /dev/null && pwd -P >| /tmp/claude-786b-cwd`;
  return ["/bin/zsh", "-c", script];
}

describe("toolCommand", () => {
  // spec:web-ui/ai-agent-session-view/tool-calls-are-labelled-with-the-command-the-agent-ran
  it.each([
    // Each command an edr-dev Claude Code session ran, as the wrapper quoted it.
    ["a pipeline", "'ls /private/tmp | head -3'", "ls /private/tmp | head -3"],
    ["a backgrounded subshell", "'(sleep 47 &)'", "(sleep 47 &)"],
    ["nohup with redirections", "'nohup sleep 48 >/dev/null 2>&1 &'", "nohup sleep 48 >/dev/null 2>&1 &"],
    ["env -i", "'env -i /bin/echo qa1280'", "env -i /bin/echo qa1280"],
    ["double quotes inside", `'/bin/sh -c "sleep 1; echo qa1280-done"'`, `/bin/sh -c "sleep 1; echo qa1280-done"`],
    // A single quote in the command closes the run, escapes a quote, and reopens it.
    ["an embedded single quote", `'echo '\\''it'\\''s'\\'''`, "echo 'it's'"],
    // The wrapper always opens a run, so a command starting with a quote begins with an empty one.
    ["a quote at the very start", `''\\''quoted'\\'' rest'`, "'quoted' rest"],
    ["an empty command", "''", ""],
    // Claude Code 2.1.300 and later escape an embedded quote as '"'"': close, a double-quoted quote, reopen (issue #1350).
    // spec:web-ui/tool-command-quoting/a-command-with-the-double-quoted-escape-reads-as-the-agent-ran-it
    ["the double-quoted escape, as edr-dev captured it",
      `'open -b com.apple.TextEdit && sleep 5 && osascript -e '"'"'quit app "TextEdit"'"'"''`,
      `open -b com.apple.TextEdit && sleep 5 && osascript -e 'quit app "TextEdit"'`],
    ["the double-quoted escape at the start", `''"'"'quoted'"'"' rest'`, "'quoted' rest"],
    ["the double-quoted escape in the middle", `'echo it'"'"'s'`, "echo it's"],
    ["the double-quoted escape at the end", `'echo '"'"''`, "echo '"],
    // The RSA rerun's first call: double quotes, a newline and the escape in one command.
    ["a multi-line command with the double-quoted escape",
      `'npm init -y >/dev/null && echo "const esbuild = require('"'"'esbuild'"'"');
console.log(esbuild.version);" > build.js'`,
      `npm init -y >/dev/null && echo "const esbuild = require('esbuild');
console.log(esbuild.version);" > build.js`],
  ])("reads %s", (_name, quoted, want) => {
    expect(toolCommand(wrap(quoted))).toBe(want);
  });

  it("reads the last eval, which is the model's, past one in the restored snapshot", () => {
    const args = ["/bin/bash", "-c", "eval 'snapshot setup' && eval 'git status' < /dev/null && pwd -P >| /tmp/claude-1-cwd"];
    expect(toolCommand(args)).toBe("git status");
  });

  it("skips an escaped quote before the eval rather than reading a quoted run from it", () => {
    expect(toolCommand(["/bin/zsh", "-c", "echo \\' && eval 'git status' < /dev/null"])).toBe("git status");
  });

  it("does not take an eval inside a double-quoted run for the wrapper's", () => {
    expect(toolCommand(["/bin/zsh", "-c", `eval 'git status' && echo "then eval 'x'"`])).toBe("git status");
  });

  it("reads the wrapper's eval when the command itself runs eval", () => {
    expect(toolCommand(wrap(`'cd /tmp && eval '\\''echo hi'\\'''`))).toBe("cd /tmp && eval 'echo hi'");
  });

  it.each([
    ["no argv", undefined],
    ["a plain exec", ["git", "status"]],
    ["a shell without -c", ["/bin/zsh", "-l"]],
    ["a shell with another flag", ["/bin/zsh", "-l", "eval 'git status'"]],
    ["a non-shell -c", ["/usr/bin/python3", "-c", "eval 'x'"]],
    ["a shell script with no eval", ["/bin/zsh", "-c", "git status"]],
    ["an unterminated quote", ["/bin/zsh", "-c", "eval 'git status"]],
    ["an unterminated double quote", ["/bin/zsh", "-c", `eval 'git'"status`]],
    ["a word that only ends in eval", ["/bin/zsh", "-c", "echo retrieval payload"]],
  ])("returns null for %s", (_name, args) => {
    expect(toolCommand(args)).toBeNull();
  });
});

describe("shellScript", () => {
  it("takes the script of any common shell by its basename", () => {
    expect(shellScript(["/opt/homebrew/bin/bash", "-c", "x"])).toBe("x");
    expect(shellScript(["sh", "-c", "y"])).toBe("y");
    expect(shellScript(["/bin/zsh", "-c", "x", "extra"])).toBeNull();
  });
});

describe("readShellWord", () => {
  it("stops at unquoted whitespace and keeps quoted whitespace", () => {
    expect(readShellWord("'a b'c d", 0)).toBe("a bc");
    expect(readShellWord("x\\ y z", 0)).toBe("x y");
  });

  it("unescapes only what POSIX escapes inside double quotes", () => {
    expect(readShellWord(String.raw`"a\$b\"c\\d\e"`, 0)).toBe(String.raw`a$b"c\d\e`);
  });

  it("drops a backslash-newline, a line continuation, in and out of double quotes, and keeps a bare newline", () => {
    expect(readShellWord('"echo a\\\nb"', 0)).toBe("echo ab");
    expect(readShellWord("echo\\\nb", 0)).toBe("echob");
    expect(readShellWord('"echo a\nb"', 0)).toBe("echo a\nb");
  });

  it("keeps a trailing backslash with nothing to escape", () => {
    expect(readShellWord("ab\\", 0)).toBe("ab\\");
  });
});

describe("commandLine", () => {
  it("joins the argv, or falls back to the path for a fork that never exec'd", () => {
    expect(commandLine("/bin/ls", ["ls", "-la"])).toBe("ls -la");
    expect(commandLine("/bin/zsh", undefined)).toBe("/bin/zsh");
    expect(commandLine("/bin/zsh", [])).toBe("/bin/zsh");
  });
});
