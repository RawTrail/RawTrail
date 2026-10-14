// Shaping an AI agent session's process forest into what the session view shows (issue #1284): one group per tool call, labelled
// with the command the agent ran, with the shell plumbing that ran it hidden unless the operator asks for it.
//
// The server reads the session as a forest built by creator. The agent process itself carries no session marker, so each tool call's
// shell is a root, and everything that call started (a pipeline's commands, a daemon it double-forked or nohup'ed) hangs under it.

import type { ProcessNode } from "../../types";
import { commandLine, isShellPath, toolCommand } from "./toolCommand";

// SessionRow is one process the view lists, with the rows under it.
export interface SessionRow {
  node: ProcessNode;
  children: SessionRow[];
}

// ToolCall is one group: the processes a single tool call started.
export interface ToolCall {
  // key identifies the group: its tool-call group when the server assigned one, else its first shell's row.
  key: string;
  // command is what the agent ran: the wrapper's eval argument, or the root's own command line when it is not such a wrapper.
  command: string;
  startNs: number;
  // lastNs is the group's last observed activity: its latest exit, exec or fork.
  lastNs: number;
  // processCount counts every process in the group, including the plumbing the view hides.
  processCount: number;
  rows: SessionRow[];
}

// SessionSummary is what the view's header says about the whole session, read from its processes.
export interface SessionSummary {
  agentName?: string;
  agentVersion?: string;
  // project is the working directory of the session's first tool call.
  project?: string;
  firstNs?: number;
  lastNs?: number;
  processCount: number;
}

function byForkTime(a: ProcessNode, b: ProcessNode): number {
  return a.fork_time_ns - b.fork_time_ns || a.id - b.id;
}

// lastActivityNs is when a process was last seen doing something. Only an observed exit counts: the server synthesizes the others
// (a stale process closed by its sweep hours later, a pid reused), and their time says when the server noticed, not when the process
// ran. A re-exec's exit is the moment the next image started.
function lastActivityNs(node: ProcessNode): number {
  const observedExit = node.exit_reason === "event" || node.exit_reason === "reexec" ? node.exit_time_ns : undefined;
  return observedExit ?? node.exec_time_ns ?? node.fork_time_ns;
}

// walk visits a node and everything under it.
function walk(node: ProcessNode, visit: (n: ProcessNode) => void): void {
  visit(node);
  for (const child of node.children ?? []) walk(child, visit);
}

// isShellCopy reports a fork of a shell that never exec'd: the copy a shell makes to run a subshell or a background job. It carries
// its creator's image and no command of its own.
function isShellCopy(node: ProcessNode, creator: ProcessNode): boolean {
  return node.exec_time_ns === undefined && node.path === creator.path && isShellPath(node.path);
}

// visibleRows lists a node's children as the view shows them. Unless verbose, a shell copy is replaced by its own children.
function visibleRows(creator: ProcessNode, verbose: boolean): SessionRow[] {
  const rows: SessionRow[] = [];
  for (const child of [...(creator.children ?? [])].sort(byForkTime)) {
    if (!verbose && isShellCopy(child, creator)) {
      rows.push(...visibleRows(child, verbose));
    } else {
      rows.push({ node: child, children: visibleRows(child, verbose) });
    }
  }
  return rows;
}

// rootRows is what a tool call's root contributes: unless verbose, a wrapper shell is the group's header rather than a row, so its
// children stand in its place.
function rootRows(root: ProcessNode, verbose: boolean): SessionRow[] {
  if (!verbose && toolCommand(root.args) !== null) return visibleRows(root, verbose);
  return [{ node: root, children: visibleRows(root, verbose) }];
}

// buildToolCalls groups the session's forest by tool call, in the order the calls started. A root whose creator was not read joins
// the group its tool-call key names, so a call stays one group even when part of its tree arrived without its parent.
export function buildToolCalls(roots: readonly ProcessNode[], verbose: boolean): ToolCall[] {
  const groups = new Map<string, ToolCall>();
  for (const root of [...roots].sort(byForkTime)) {
    const key = root.tool_call_group === undefined ? `row:${String(root.id)}` : `call:${String(root.tool_call_group)}`;
    let group = groups.get(key);
    if (group === undefined) {
      group = {
        key,
        command: toolCommand(root.args) ?? commandLine(root.path, root.args),
        startNs: root.fork_time_ns,
        lastNs: root.fork_time_ns,
        processCount: 0,
        rows: [],
      };
      groups.set(key, group);
    }
    const into = group;
    walk(root, (n) => {
      into.processCount += 1;
      into.lastNs = Math.max(into.lastNs, lastActivityNs(n));
    });
    into.rows.push(...rootRows(root, verbose));
  }
  return [...groups.values()];
}

// summarizeSession reads the header's facts from the session's processes. The agent is named on every stamped process; the project
// is where the first tool call ran.
export function summarizeSession(roots: readonly ProcessNode[]): SessionSummary {
  const summary: SessionSummary = { processCount: 0 };
  const first = [...roots].sort(byForkTime)[0] as ProcessNode | undefined;
  summary.project = first?.cwd;
  for (const root of roots) {
    walk(root, (n) => {
      summary.processCount += 1;
      summary.agentName ??= n.agent_name;
      summary.agentVersion ??= n.agent_version;
      summary.firstNs = Math.min(summary.firstNs ?? n.fork_time_ns, n.fork_time_ns);
      summary.lastNs = Math.max(summary.lastNs ?? 0, lastActivityNs(n));
    });
  }
  return summary;
}
