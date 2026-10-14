import { describe, it, expect } from "vitest";

import { buildToolCalls, summarizeSession, type RowShape, type SessionRow } from "./sessionTree";
import { MS, proc, realSession } from "../../test/agentSession";

const QUIET: RowShape = { verbose: false };
const VERBOSE: RowShape = { verbose: true };

// outline renders rows as indented "path" lines, so a test reads the shape the view shows.
function outline(rows: SessionRow[], depth = 0): string[] {
  return rows.flatMap((r) => [`${"  ".repeat(depth)}${r.node.path}`, ...outline(r.children, depth + 1)]);
}

describe("buildToolCalls", () => {
  // spec:web-ui/ai-agent-session-view/tool-calls-are-labelled-with-the-command-the-agent-ran
  it("makes one group per tool call, in the order they ran, labelled with the agent's command", () => {
    const calls = buildToolCalls(realSession(), QUIET);
    expect(calls.map((c) => c.command)).toEqual([
      "ls /private/tmp | head -3",
      "(sleep 47 &)",
      "nohup sleep 48 >/dev/null 2>&1 &",
      "env -i /bin/echo qa1280",
      `/bin/sh -c "sleep 1; echo qa1280-done"`,
    ]);
    expect(calls.map((c) => c.key)).toEqual(["row:2298322", "row:2298325", "row:2298439", "row:2298442", "row:2298447"]);
  });

  // spec:web-ui/ai-agent-session-view/a-daemon-the-agent-started-stays-in-its-tool-call
  it("keeps a backgrounded daemon inside the call that started it and hides the subshell copy that started it", () => {
    const [, subshell] = buildToolCalls(realSession(), QUIET);
    expect(outline(subshell.rows)).toEqual(["/bin/sleep"]);
    // The daemon outlived the call, and the group's span reaches its exit.
    const nohup = buildToolCalls(realSession(), QUIET)[2];
    expect(outline(nohup.rows)).toEqual(["/usr/bin/nohup", "/bin/sleep"]);
    expect(nohup.lastNs).toBe(350_000 * MS);
  });

  // spec:web-ui/ai-agent-session-view/shell-plumbing-is-hidden-unless-verbose
  it("lists the wrapper shell and the subshell copy only when verbose", () => {
    const quiet = buildToolCalls(realSession(), QUIET);
    expect(outline(quiet[0].rows)).toEqual(["/bin/ls", "/usr/bin/head"]);
    expect(quiet[1].processCount).toBe(3);

    const verbose = buildToolCalls(realSession(), VERBOSE);
    expect(outline(verbose[0].rows)).toEqual(["/bin/zsh", "  /bin/ls", "  /usr/bin/head"]);
    expect(outline(verbose[1].rows)).toEqual(["/bin/zsh", "  /bin/zsh", "    /bin/sleep"]);
    expect(verbose.map((c) => c.command)).toEqual(quiet.map((c) => c.command));
  });

  it("does not stretch a call's span to an exit the server synthesized", () => {
    const daemon = proc(3, 3, 202, "/bin/sleep", ["sleep", "47"], [], { exit_reason: "ttl_reconciliation", exit_time_ns: 9_000_000 * MS });
    const root = proc(2, 2, 200, "/bin/zsh", ["zsh", "-c", "eval '(sleep 47 &)'"], [daemon], { tool_call_group: 2 });
    // The shell's observed exit is the call's last activity, not the sweep that closed the sleep hours later.
    expect(buildToolCalls([root], QUIET)[0].lastNs).toBe(205 * MS);
  });

  // spec:web-ui/ai-agent-session-view-badges/an-alerted-process-is-never-hidden-as-plumbing
  it("lists plumbing an alert was raised on, even with verbose off", () => {
    const keep = new Set([2298322, 2298326]);
    const calls = buildToolCalls(realSession(), { verbose: false, keep });
    expect(outline(calls[0].rows)).toEqual(["/bin/zsh", "  /bin/ls", "  /usr/bin/head"]);
    expect(outline(calls[1].rows)).toEqual(["/bin/zsh", "  /bin/sleep"]);
  });

  // spec:web-ui/ai-agent-session-view-badges/a-privilege-change-is-badged
  it("marks a process that runs as a different user from its creator", () => {
    const root = proc(2, 2, 200, "/bin/zsh", ["/bin/zsh", "-c", "eval 'sudo id'"], [
      proc(3, 3, 201, "/usr/bin/sudo", ["sudo", "id"], [proc(4, 4, 202, "/usr/bin/id", ["id"], [], { uid: 0 })], { uid: 0 }),
    ], { tool_call_group: 2, uid: 501 });
    const [call] = buildToolCalls([root], QUIET);
    const [sudo] = call.rows;
    expect(sudo.uidChange).toEqual({ from: 501, to: 0 });
    expect(sudo.children[0].uidChange).toBeUndefined();
  });

  it("lists a shell copy that runs as another user, since that is a privilege change", () => {
    const copy = proc(3, 3, 201, "/bin/zsh", undefined, [proc(4, 4, 202, "/bin/sleep", ["sleep", "1"], [], { uid: 0 })], { uid: 0 });
    const root = proc(2, 2, 200, "/bin/zsh", ["/bin/zsh", "-c", "eval '(sleep 1 &)'"], [copy], { tool_call_group: 2, uid: 501 });
    const [call] = buildToolCalls([root], QUIET);
    expect(outline(call.rows)).toEqual(["/bin/zsh", "  /bin/sleep"]);
    expect(call.rows[0].uidChange).toEqual({ from: 501, to: 0 });
  });

  it("compares a hoisted process with its real creator, not the row it is listed under", () => {
    // The shell copy is hidden, so the sleep is listed under the call; its creator is still the copy, which ran as root.
    const copy = proc(3, 3, 201, "/bin/zsh", undefined, [proc(4, 4, 202, "/bin/sleep", ["sleep", "1"], [], { uid: 0 })], { uid: 0 });
    const root = proc(2, 2, 200, "/bin/zsh", ["/bin/zsh", "-c", "eval '(sleep 1 &)'"], [copy], { tool_call_group: 2, uid: 0 });
    expect(buildToolCalls([root], QUIET)[0].rows[0].uidChange).toBeUndefined();
  });

  it("collects every process of a call, hidden plumbing included", () => {
    expect(buildToolCalls(realSession(), QUIET)[1].processIds).toEqual([2298325, 2298326, 2298327]);
  });

  it("keeps a fork that never exec'd when it is not a shell copy", () => {
    const worker = proc(11, 2, 11, "/usr/bin/python3", undefined);
    const root = proc(10, 1, 10, "/usr/bin/python3", ["python3", "serve.py"], [worker], { tool_call_group: 1 });
    const [call] = buildToolCalls([root], QUIET);
    expect(call.command).toBe("python3 serve.py");
    expect(outline(call.rows)).toEqual(["/usr/bin/python3", "  /usr/bin/python3"]);
  });

  it("keeps a fork that never exec'd when it is a different shell from its creator", () => {
    const fork = proc(11, 2, 11, "/bin/bash", undefined);
    const root = proc(10, 1, 10, "/bin/zsh", ["/bin/zsh", "-c", "eval 'bash -c x'"], [fork], { tool_call_group: 1 });
    expect(outline(buildToolCalls([root], QUIET)[0].rows)).toEqual(["/bin/bash"]);
  });

  it("folds a root whose creator was not read into its tool call's group", () => {
    const orphan = proc(9, 9, 150, "/usr/bin/git", ["git", "status"], [], { tool_call_group: 80727 });
    const calls = buildToolCalls([...realSession(), orphan], QUIET);
    expect(calls).toHaveLength(5);
    expect(outline(calls[0].rows)).toEqual(["/bin/ls", "/usr/bin/head", "/usr/bin/git"]);
    expect(calls[0].processCount).toBe(4);
  });

  it("starts a new group when a later call's leader reuses an earlier call's process group", () => {
    const first = proc(1, 700, 100, "/bin/zsh", ["/bin/zsh", "-c", "eval 'git status'"], [], { tool_call_group: 700 });
    const reused = proc(2, 700, 900, "/bin/zsh", ["/bin/zsh", "-c", "eval 'make test'"], [], { tool_call_group: 700 });
    // A process of the later call whose creator was not read joins the later call, not the earlier one.
    const orphan = proc(3, 701, 950, "/usr/bin/make", ["make", "test"], [], { tool_call_group: 700 });
    const calls = buildToolCalls([orphan, reused, first], QUIET);
    expect(calls.map((c) => c.command)).toEqual(["git status", "make test"]);
    expect(outline(calls[1].rows)).toEqual(["/usr/bin/make"]);
  });

  it("gives a root without a tool-call group a group of its own, labelled with its command line", () => {
    const hook = proc(7, 7, 50, "/usr/bin/osascript", ["osascript", "-e", "beep"]);
    const [first] = buildToolCalls([hook, ...realSession()], QUIET);
    expect(first.key).toBe("row:7");
    expect(first.command).toBe("osascript -e beep");
    expect(outline(first.rows)).toEqual(["/usr/bin/osascript"]);
  });
});

describe("summarizeSession", () => {
  it("names the agent, the first call's project, the span and every process", () => {
    expect(summarizeSession(realSession())).toEqual({
      agentName: "claude-code",
      agentVersion: "2.1.296",
      project: "/private/tmp",
      firstNs: 100 * MS,
      lastNs: 350_000 * MS,
      processCount: 16, // the capture's 16, as the sessions list counts them
    });
  });

  it("takes the project from the call that ran first, whatever order the forest arrives in", () => {
    const later = proc(2, 2, 200, "/bin/zsh", ["zsh"], [], { cwd: "/Users/dev/other" });
    const earlier = proc(1, 1, 100, "/bin/zsh", ["zsh"], [], { cwd: "/Users/dev/project" });
    expect(summarizeSession([later, earlier]).project).toBe("/Users/dev/project");
  });

  it("is empty for an empty session", () => {
    expect(summarizeSession([])).toEqual({ processCount: 0 });
  });
});
