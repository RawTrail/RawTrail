// A real AI agent session's process forest, for the session view's tests (issue #1284).

import type { ProcessLink, ProcessNode, SessionCommand, SessionCommandsResponse } from "../types";
import { applyLaunchLinks } from "../components/launchLinks";
import { commandLine, toolCommand } from "../components/Sessions/toolCommand";

export const SESSION = "169b5bb1-29b5-41da-9a54-92cd72599ad9";
export const MS = 1_000_000;

// wrapper is the shell Claude Code runs one Bash tool call in, as captured on edr-dev.
function wrapper(id: number, pid: number, atMs: number, quotedCommand: string, children: ProcessNode[]): ProcessNode {
  const script =
    "source /Users/victor/.claude/shell-snapshots/snapshot-zsh-1791902227385-pju9os.sh 2>/dev/null || true && " +
    `eval ${quotedCommand} < /dev/null && pwd -P >| /tmp/claude-786b-cwd`;
  return proc(id, pid, atMs, "/bin/zsh", ["/bin/zsh", "-c", script], children, { tool_call_group: pid, cwd: "/private/tmp" });
}

export function proc(
  id: number,
  pid: number,
  atMs: number,
  path: string,
  args: string[] | undefined,
  children: ProcessNode[] = [],
  over: Partial<ProcessNode> = {},
): ProcessNode {
  return {
    id,
    host_id: "H1",
    pid,
    ppid: 0,
    path,
    args,
    fork_time_ns: atMs * MS,
    // A fork that never exec'd carries no argv, no exec time and no user; everything the capture exec'd ran as uid 501.
    exec_time_ns: args === undefined ? undefined : atMs * MS + 1,
    uid: args === undefined ? undefined : 501,
    exit_time_ns: atMs * MS + 5 * MS,
    exit_reason: "event",
    agent_session_id: SESSION,
    agent_name: "claude-code",
    agent_version: "2.1.296",
    children,
    ...over,
  };
}

// realSession is the forest the server returns for the session captured on edr-dev (Claude Code 2.1.296): five Bash tool calls, each
// a root because the agent process itself carries no session marker. Row ids, pids and shapes are the capture's; times are relative.
// The roots arrive out of order, as a forest read gives no order guarantee.
export function realSession(): ProcessNode[] {
  return [
    wrapper(2298439, 80838, 300, "'nohup sleep 48 >/dev/null 2>&1 &'", [
      proc(2298440, 80839, 301, "/usr/bin/nohup", ["nohup", "sleep", "48"], [], { exit_reason: "reexec" }),
      proc(2298441, 80839, 302, "/bin/sleep", ["sleep", "48"], [], { previous_exec_id: 2298440, exit_time_ns: 350_000 * MS }),
    ]),
    wrapper(2298322, 80727, 100, "'ls /private/tmp | head -3'", [
      proc(2298324, 80729, 102, "/usr/bin/head", ["head", "-3"]),
      proc(2298323, 80728, 101, "/bin/ls", ["ls", "/private/tmp"]),
    ]),
    wrapper(2298325, 80730, 200, "'(sleep 47 &)'", [
      // The subshell's fork of zsh, which never exec'd, then the sleep it backgrounded and that outlived the call.
      proc(2298326, 80731, 201, "/bin/zsh", undefined, [proc(2298327, 80732, 202, "/bin/sleep", ["sleep", "47"])]),
    ]),
    wrapper(2298442, 80840, 400, "'env -i /bin/echo qa1280'", [
      proc(2298444, 80842, 401, "/usr/bin/env", ["env", "-i", "/bin/echo", "qa1280"], [], { exit_reason: "reexec" }),
      proc(2298445, 80842, 402, "/bin/echo", ["/bin/echo", "qa1280"], [], { previous_exec_id: 2298444 }),
    ]),
    wrapper(2298447, 80843, 500, `'/bin/sh -c "sleep 1; echo qa1280-done"'`, [
      proc(2298448, 80844, 501, "/bin/sh", ["/bin/sh", "-c", "sleep 1; echo qa1280-done"], [], { exit_reason: "reexec" }),
      proc(2298449, 80844, 502, "/bin/bash", ["/bin/sh", "-c", "sleep 1; echo qa1280-done"], [
        proc(2298450, 80845, 503, "/bin/sleep", ["sleep", "1"]),
      ], { previous_exec_id: 2298448 }),
    ]),
  ];
}

// sessionCommands is the server's commands response for a session's forest (issue #1423), as test fixtures need it: what launchd
// started moved under its launcher, then one command per leader of a tool-call group, with any root whose creator was not read joining
// the latest command its key names. The server's grouping is tested in Go; this only builds realistic responses for the view.
export function sessionCommands(
  roots: readonly ProcessNode[], links?: readonly ProcessLink[], over: Partial<SessionCommandsResponse> = {},
): SessionCommandsResponse {
  const logical = applyLaunchLinks(roots, links);
  const commands: SessionCommand[] = [];
  const latestByKey = new Map<number, SessionCommand>();
  const sorted = [...logical.roots].sort((a, b) => a.fork_time_ns - b.fork_time_ns || a.id - b.id);
  for (const root of sorted) {
    const key = root.tool_call_group;
    let command = key === undefined || root.pid === key ? undefined : latestByKey.get(key);
    if (command === undefined) {
      const agentCommand = toolCommand(root.args);
      command = {
        key: `row:${String(root.id)}`, command: agentCommand ?? commandLine(root.path, root.args), from_agent: agentCommand !== null,
        uid: root.uid, start_ns: root.fork_time_ns, last_ns: root.fork_time_ns, process_ids: [], roots: [],
      };
      commands.push(command);
      if (key !== undefined) latestByKey.set(key, command);
    }
    const into = command;
    const visit = (n: ProcessNode) => {
      into.process_ids.push(n.id);
      const observedExit = n.exit_reason === "event" || n.exit_reason === "reexec" ? n.exit_time_ns : undefined;
      into.last_ns = Math.max(into.last_ns, observedExit ?? n.exec_time_ns ?? n.fork_time_ns);
      for (const child of n.children ?? []) visit(child);
    };
    visit(root);
    into.roots.push(root);
  }
  return {
    commands, total_matched: commands.length, inferred: [...logical.inferred], services: [...logical.services], truncated: false,
    tool_calls_truncated: false, ...over,
  };
}
