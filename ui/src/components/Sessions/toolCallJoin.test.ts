import { describe, it, expect } from "vitest";

import { joinToolCalls } from "./toolCallJoin";
import type { Command } from "./sessionTree";
import type { AgentToolCall } from "../../types";

const MS = 1_000_000;

const UID = 501;

function group(key: string, command: string, startNs: number, fromAgent = true): Command {
  return { key, command, fromAgent, uid: UID, startNs, lastNs: startNs, processCount: 1, processIds: [], rows: [] };
}

function call(id: string, command: string, atNs: number, over: Partial<AgentToolCall> = {}): AgentToolCall {
  return { tool_use_id: id, tool_name: "Bash", command, called_at_ns: atNs, uid: UID, ...over };
}

describe("joinToolCalls", () => {
  // spec:web-ui/tool-calls-beside-their-processes/repeated-identical-commands-pair-in-order
  it("pairs repeated identical commands in order, as the edr-dev session ran ls, pwd, ls", () => {
    // Times from the edr-dev session: each shell started 5 to 70 ms after its call.
    const groups = [
      group("g1", "ls", 1_000 * MS + 69 * MS), group("g2", "pwd", 2_000 * MS + 5 * MS), group("g3", "ls", 3_000 * MS + 25 * MS),
    ];
    const calls = [call("toolu_a", "ls", 1_000 * MS), call("toolu_b", "pwd", 2_000 * MS), call("toolu_c", "ls", 3_000 * MS)];
    const matches = joinToolCalls(groups, calls);
    expect(matches.get("g1")).toEqual({ kind: "matched", call: calls[0] });
    expect(matches.get("g2")).toEqual({ kind: "matched", call: calls[1] });
    expect(matches.get("g3")).toEqual({ kind: "matched", call: calls[2] });
  });

  // spec:web-ui/tool-calls-beside-their-processes/another-user-s-transcript-cannot-label-a-session
  it("pairs a call only with a group its transcript's owner ran", () => {
    // Another user wrote a line with the same command into their own transcript, just before the real call's shell started.
    const real = call("toolu_real", "ls", 1_000 * MS);
    const forged = call("toolu_forged", "ls", 1_005 * MS, { uid: 502 });
    expect(joinToolCalls([group("g", "ls", 1_010 * MS)], [real, forged]).get("g")).toEqual({ kind: "matched", call: real });
    expect(joinToolCalls([group("g", "ls", 1_010 * MS)], [forged]).has("g")).toBe(false);
  });

  it("lets a set of tied calls label only as many groups as it holds", () => {
    const calls = [call("toolu_a", "ls", 1_000 * MS), call("toolu_b", "ls", 1_000 * MS)];
    const matches = joinToolCalls([group("g1", "ls", 1_010 * MS), group("g2", "ls", 1_012 * MS), group("g3", "ls", 9_000 * MS)], calls);
    expect(matches.get("g1")?.kind).toBe("ambiguous");
    expect(matches.get("g2")?.kind).toBe("ambiguous");
    expect(matches.has("g3")).toBe(false);
  });

  it("pairs a call with one group at most", () => {
    // A transcript line the agent could not read leaves the second ls without its call; it must not take the first group's.
    const calls = [call("toolu_a", "ls", 1_000 * MS)];
    const matches = joinToolCalls([group("g1", "ls", 1_010 * MS), group("g2", "ls", 4_010 * MS)], calls);
    expect(matches.get("g1")).toEqual({ kind: "matched", call: calls[0] });
    expect(matches.has("g2")).toBe(false);
  });

  it("passes over a call that ran nothing, such as one the user refused", () => {
    const calls = [call("refused", "rm -rf build", 1_000 * MS), call("approved", "rm -rf build", 5_000 * MS)];
    expect(joinToolCalls([group("g", "rm -rf build", 5_010 * MS)], calls).get("g")).toEqual({ kind: "matched", call: calls[1] });
  });

  it("waits out an approval, but not past an hour", () => {
    const calls = [call("toolu_a", "make", 0)];
    expect(joinToolCalls([group("g", "make", 30 * 60_000 * MS)], calls).get("g")?.kind).toBe("matched");
    expect(joinToolCalls([group("g", "make", 61 * 60_000 * MS)], calls).has("g")).toBe(false);
  });

  // spec:web-ui/tool-calls-beside-their-processes/calls-that-cannot-be-told-apart-are-marked-ambiguous
  it("marks identical calls of one turn ambiguous rather than guessing", () => {
    const calls = [call("toolu_a", "ls", 1_000 * MS), call("toolu_b", "ls", 1_000 * MS)];
    const matches = joinToolCalls([group("g1", "ls", 1_010 * MS), group("g2", "ls", 1_012 * MS)], calls);
    for (const key of ["g1", "g2"]) expect(matches.get(key)).toEqual({ kind: "ambiguous", candidates: calls });
  });

  it("matches a cut command by its start, and only a Bash call with the group an agent's shell started", () => {
    const long = `cat <<'EOF'\n${"x".repeat(20)}\nEOF`;
    const cut = call("toolu_cut", long.slice(0, 10), 1_000 * MS, { truncated: true });
    expect(joinToolCalls([group("g", long, 1_020 * MS)], [cut]).get("g")).toEqual({ kind: "matched", call: cut });
    const edit = call("toolu_edit", "ls", 1_000 * MS, { tool_name: "Edit" });
    expect(joinToolCalls([group("g", "ls", 1_020 * MS)], [edit]).size).toBe(0);
    expect(joinToolCalls([group("g", "ls", 1_020 * MS, false)], [call("toolu_a", "ls", 1_000 * MS)]).size).toBe(0);
    expect(joinToolCalls([group("g", "ls", 990 * MS)], [call("toolu_a", "ls", 1_000 * MS)]).size).toBe(0);
  });

  // spec:web-ui/tool-calls-beside-their-processes/repeated-identical-commands-pair-in-order
  it("holds its invariants over random sessions", () => {
    let seed = 1287;
    const random = () => {
      seed = (seed * 1_103_515_245 + 12_345) % 2_147_483_648;
      return seed / 2_147_483_648;
    };
    const commands = ["ls", "pwd", "git status"];
    for (let round = 0; round < 300; round += 1) {
      const calls: AgentToolCall[] = [];
      const groups: Command[] = [];
      const truth = new Map<string, string>();
      let t = 0;
      for (let i = 0; i < Math.floor(random() * 12); i += 1) {
        // Calls of one turn share its time exactly; turns are at least a model round trip apart, longer than a shell takes to start.
        t += Math.floor(random() * 3) * 1_000 * MS;
        const command = commands[Math.floor(random() * commands.length)];
        const id = `toolu_${String(round)}_${String(i)}`;
        calls.push(call(id, command, t));
        if (random() < 0.8) {
          const key = `g_${id}`;
          groups.push(group(key, command, t + 5 * MS + Math.floor(random() * 60) * MS));
          truth.set(key, id);
        }
      }
      const matches = joinToolCalls(groups, calls);
      const used = new Set<string>();
      const byKey = new Map(groups.map((g) => [g.key, g]));
      for (const [key, match] of matches) {
        const g = byKey.get(key);
        expect(g).toBeDefined();
        if (match.kind === "ambiguous") {
          expect(match.candidates.length).toBeGreaterThan(1);
          expect(new Set(match.candidates.map((c) => c.called_at_ns)).size).toBe(1);
          continue;
        }
        expect(used.has(match.call.tool_use_id)).toBe(false);
        used.add(match.call.tool_use_id);
        expect(match.call.command).toBe(g?.command);
        expect(match.call.called_at_ns).toBeLessThanOrEqual(g?.startNs ?? 0);
      }
      // When no two calls share a time, every group is paired with the call that really started it.
      if (new Set(calls.map((c) => c.called_at_ns)).size === calls.length) {
        for (const [key, id] of truth) expect(matches.get(key)).toEqual({ kind: "matched", call: calls.find((c) => c.tool_use_id === id) });
      }
    }
  });
});
