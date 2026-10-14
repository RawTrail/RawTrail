// Pairing each tool-call group of a session with the tool call that started it (issue #1288).
//
// The agent records a call in its transcript, then runs its shell. A group's command is the shell's eval argument, so a call pairs with
// a group that ran its command and started after it. A call the user took minutes to approve starts its shell minutes later, so the
// pairing is by order rather than by a tight window: each group takes the nearest unclaimed call before it that ran its command.
// A call with no group (one the user refused) is passed over by the group after it, which takes a later call.
//
// A transcript is its user's to write, so a call pairs only with a group its owner ran: another user cannot label this session's
// processes by writing lines into their own transcript.
//
// Calls the agent made in one turn share the turn's time, so identical commands in one turn cannot be told apart by time. Their groups
// are marked ambiguous, naming the candidates, rather than paired by a guess.

import type { AgentToolCall } from "../../types";
import type { ToolCall } from "./sessionTree";

// MATCH_WINDOW_NS is the longest a group may start after its call: an hour of waiting for the user's approval.
const MATCH_WINDOW_NS = 3_600_000_000_000;

export type ToolCallMatch =
  | { kind: "matched"; call: AgentToolCall }
  | { kind: "ambiguous"; candidates: AgentToolCall[] };

// runs reports whether a group ran a call's command. A command cut to the agent's limit matches the group whose command begins with it.
function runs(call: AgentToolCall, group: ToolCall): boolean {
  if (!call.command) return false;
  return call.truncated ? group.command.startsWith(call.command) : group.command === call.command;
}

// joinToolCalls pairs each of the groups an agent's wrapper shell started with its Bash call, keyed by group. A group with no call
// before it that ran its command is left out.
export function joinToolCalls(groups: readonly ToolCall[], calls: readonly AgentToolCall[]): Map<string, ToolCallMatch> {
  const bash = calls.filter((c) => c.tool_name === "Bash");
  const claimed = new Set<string>();
  const matches = new Map<string, ToolCallMatch>();
  for (const group of [...groups].filter((g) => g.fromAgent).sort((a, b) => a.startNs - b.startNs)) {
    const candidates = bash.filter((c) => !claimed.has(c.tool_use_id) && c.uid === group.uid && runs(c, group)
      && c.called_at_ns <= group.startNs && group.startNs - c.called_at_ns <= MATCH_WINDOW_NS);
    if (candidates.length === 0) continue;
    const nearest = Math.max(...candidates.map((c) => c.called_at_ns));
    const tied = candidates.filter((c) => c.called_at_ns === nearest);
    if (tied.length > 1) {
      matches.set(group.key, { kind: "ambiguous", candidates: tied });
      continue;
    }
    claimed.add(tied[0].tool_use_id);
    matches.set(group.key, { kind: "matched", call: tied[0] });
  }
  return matches;
}
