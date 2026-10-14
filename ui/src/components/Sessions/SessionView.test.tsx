import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import { render, screen, fireEvent, within } from "@testing-library/react";
import { MemoryRouter, Routes, Route, Link } from "react-router";

import { SessionView } from "./SessionView";
import * as api from "../../api";
import { proc, realSession, SESSION } from "../../test/agentSession";
import type { AgentSessionSummary, Alert, ProcessNode, TreeResponse } from "../../types";

function tree(over: Partial<TreeResponse> = {}): TreeResponse {
  return { roots: realSession(), returned: 16, total_matched: 16, total_matched_capped: false, truncated: false, ...over };
}

function renderView() {
  return render(
    <MemoryRouter initialEntries={[`/sessions/H1/${SESSION}`]}>
      <Routes>
        <Route path="/sessions/:hostId/:sessionId" element={<SessionView />} />
      </Routes>
    </MemoryRouter>,
  );
}

// callHeaders returns the tool-call group headers, in the order the page lists them.
function callHeaders(): HTMLElement[] {
  return screen.getAllByRole("button", { expanded: true });
}

// The record and alert reads decorate the tree; by default the session has a record and no alerts, and each test overrides what it
// is about.
beforeEach(() => {
  vi.spyOn(api, "listAgentSessions").mockResolvedValue({ rows: [], total_matched: 0 });
  vi.spyOn(api, "listAlerts").mockResolvedValue([]);
});

afterEach(() => { vi.restoreAllMocks(); });

function record(over: Partial<AgentSessionSummary> = {}): AgentSessionSummary {
  return {
    host_id: "H1",
    agent_session_id: SESSION,
    agent_name: "claude-code",
    agent_version: "2.1.296",
    agent_pid: 80673,
    cwd: "/Users/dev/project",
    first_seen_ns: 50_000_000,
    last_seen_ns: 400_000_000_000,
    process_count: 16,
    alert_count: 1,
    ...over,
  };
}

function alert(over: Partial<Alert>): Alert {
  return {
    id: 2047,
    host_id: "H1",
    rule_id: "discovery",
    source: "detection",
    severity: "high",
    title: "System discovery",
    description: "d",
    process_id: 2298323,
    status: "open",
    created_at: "2026-10-13T09:37:07Z",
    ...over,
  } as Alert;
}

// manyCalls is a session of n tool calls, each one wrapper shell running one command, numbered from 0.
function manyCalls(n: number): ProcessNode[] {
  return Array.from({ length: n }, (_, i) => proc(10 * (i + 1), 1000 + i, 100 * (i + 1), "/bin/zsh",
    ["/bin/zsh", "-c", `eval 'step ${String(i)}'`],
    [proc(10 * (i + 1) + 1, 2000 + i, 100 * (i + 1) + 1, "/bin/echo", ["echo", String(i)])], { tool_call_group: 1000 + i }));
}

describe("SessionView", () => {
  // spec:web-ui/ai-agent-session-view/tool-calls-are-labelled-with-the-command-the-agent-ran
  it("reads the session and lists one group per tool call, labelled with its command", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([{ host_id: "H1", hostname: "qa-1280-vm" } as never]);
    const spy = vi.spyOn(api, "getSessionTree").mockResolvedValue(tree());
    renderView();
    expect(await screen.findByRole("heading", { name: "claude-code 2.1.296" })).toBeVisible();
    expect(spy).toHaveBeenCalledWith("H1", SESSION);
    expect(callHeaders().map((h) => h.querySelector("code")?.textContent)).toEqual([
      "ls /private/tmp | head -3",
      "(sleep 47 &)",
      "nohup sleep 48 >/dev/null 2>&1 &",
      "env -i /bin/echo qa1280",
      `/bin/sh -c "sleep 1; echo qa1280-done"`,
    ]);
    expect(screen.getByText("/private/tmp")).toBeVisible();
    expect(await screen.findByText("qa-1280-vm")).toBeVisible();
    expect(screen.getByText("16 processes")).toBeVisible();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  // spec:web-ui/ai-agent-session-view/shell-plumbing-is-hidden-unless-verbose
  it("shows the wrapper shells only with verbose on", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(api, "getSessionTree").mockResolvedValue(tree());
    renderView();
    const firstCall = (await screen.findByText("ls /private/tmp | head -3")).closest("li");
    if (!firstCall) throw new Error("no group");
    expect(within(firstCall).getAllByRole("link").map((l) => l.textContent)).toEqual(["ls", "head"]);

    fireEvent.click(screen.getByRole("switch", { name: "Show shell plumbing" }));
    expect(within(firstCall).getAllByRole("link").map((l) => l.textContent)).toEqual(["zsh", "ls", "head"]);
  });

  it("collapses and expands a tool call", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(api, "getSessionTree").mockResolvedValue(tree());
    renderView();
    const header = (await screen.findByText("ls /private/tmp | head -3")).closest("button");
    if (!header) throw new Error("no header");
    expect(screen.getByRole("link", { name: "ls" })).toBeVisible();
    fireEvent.click(header);
    expect(header).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByRole("link", { name: "ls" })).not.toBeInTheDocument();
  });

  it("opens a process in its host's graph, selected at its fork time", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(api, "getSessionTree").mockResolvedValue(tree());
    renderView();
    // The fixture's ls forked at 101 ms; the anchor is the next millisecond, so the fork is inside the window.
    expect(await screen.findByRole("link", { name: "ls" })).toHaveAttribute("href", "/hosts/H1?process=2298323&at=102");
  });

  it("says when the session has more processes than were read", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(api, "getSessionTree").mockResolvedValue(tree({ truncated: true, returned: 16, total_matched: 2500 }));
    renderView();
    expect(await screen.findByRole("status")).toHaveTextContent("Showing the first 16 of 2,500 processes in this session.");
  });

  // spec:web-ui/ai-agent-session-view-badges/the-header-reads-the-session-s-record
  it("reads the session's record for the header", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(api, "getSessionTree").mockResolvedValue(tree());
    // The record names the agent its markers reported most recently, which wins over what the processes carry.
    const latest = record({ agent_name: "claude", agent_version: "2.1.297" });
    const spy = vi.spyOn(api, "listAgentSessions").mockResolvedValue({ rows: [latest], total_matched: 1 });
    renderView();
    expect(await screen.findByText("agent pid 80673")).toBeVisible();
    expect(screen.getByRole("heading", { name: "claude 2.1.297" })).toBeVisible();
    expect(spy).toHaveBeenCalledWith({ host_id: "H1", session: SESSION });
    // The record's project wins over the first call's working directory.
    expect(screen.getByText("/Users/dev/project")).toBeVisible();
    expect(screen.queryByText("/private/tmp")).not.toBeInTheDocument();
  });

  // spec:web-ui/ai-agent-session-view-badges/retention-is-reported-when-the-session-began-before-it
  it.each([
    ["began before the retention cutoff", 60_000_000, true],
    ["began after it", 40_000_000, false],
  ])("reports retention only when the session %s", async (_name, retainedFrom, shown) => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(api, "getSessionTree").mockResolvedValue(tree({ retained_from_ns: retainedFrom }));
    vi.spyOn(api, "listAgentSessions").mockResolvedValue({ rows: [record({ first_seen_ns: 50_000_000 })], total_matched: 1 });
    renderView();
    expect(await screen.findByText("agent pid 80673")).toBeVisible();
    expect(screen.queryByText(/removed by retention/) !== null).toBe(shown);
  });

  // spec:web-ui/ai-agent-session-view-badges/alerts-are-badged-where-they-were-raised
  it("badges an alert on its process and its tool call, linking to the alert", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(api, "getSessionTree").mockResolvedValue(tree());
    const spy = vi.spyOn(api, "listAlerts").mockResolvedValue([alert({ severity: "critical" })]);
    renderView();
    const link = await screen.findByRole("link", { name: "System discovery" });
    expect(link).toHaveAttribute("href", "/alerts/2047");
    expect(spy).toHaveBeenCalledWith({ host_id: "H1", agent_session: SESSION, limit: 1000 });
    const header = screen.getByText("ls /private/tmp | head -3").closest("button");
    if (!header) throw new Error("no header");
    expect(within(header).getByText("1 alert")).toHaveClass("badge--critical");
  });

  // spec:web-ui/ai-agent-session-view-badges/an-alerted-process-is-never-hidden-as-plumbing
  it("lists a wrapper shell an alert was raised on, with verbose off", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(api, "getSessionTree").mockResolvedValue(tree());
    vi.spyOn(api, "listAlerts").mockResolvedValue([alert({ process_id: 2298322 })]);
    renderView();
    const firstCall = (await screen.findByRole("link", { name: "System discovery" })).closest(".session-view__call");
    if (!(firstCall instanceof HTMLElement)) throw new Error("no group");
    expect(within(firstCall).getAllByRole("link").map((l) => l.textContent)).toEqual(["zsh", "System discovery", "ls", "head"]);
  });

  // spec:web-ui/ai-agent-session-view-badges/a-long-session-opens-on-its-alerts
  it("opens a long session with only the calls an alert was raised in expanded", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(api, "getSessionTree").mockResolvedValue(tree({ roots: manyCalls(11) }));
    vi.spyOn(api, "listAlerts").mockResolvedValue([alert({ process_id: 71 })]);
    renderView();
    await screen.findByRole("link", { name: "System discovery" });
    expect(screen.getAllByRole("button", { expanded: true }).map((b) => b.querySelector("code")?.textContent)).toEqual(["step 6"]);
    expect(screen.getAllByRole("button", { expanded: false })).toHaveLength(10);
  });

  it("opens a short session with every call expanded", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(api, "getSessionTree").mockResolvedValue(tree({ roots: manyCalls(10) }));
    renderView();
    await screen.findByText("step 9");
    expect(screen.getAllByRole("button", { expanded: true })).toHaveLength(10);
  });

  // spec:web-ui/ai-agent-session-view-badges/a-privilege-change-is-badged
  it("badges a process that runs as root under a user's process", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    const sudoCall = proc(10, 10, 100, "/bin/zsh", ["/bin/zsh", "-c", "eval 'sudo id'"], [
      proc(11, 11, 101, "/usr/bin/sudo", ["sudo", "id"], [proc(12, 12, 102, "/usr/bin/id", ["id"], [], { uid: 0 })], { uid: 501 }),
    ], { tool_call_group: 10, uid: 501 });
    vi.spyOn(api, "getSessionTree").mockResolvedValue(tree({ roots: [sudoCall] }));
    renderView();
    const badge = await screen.findByText("runs as root");
    expect(badge).toBeVisible();
    expect(badge.closest("li")?.querySelector("a")?.textContent).toBe("id");
  });

  it("says so when the session's alerts could not be read, and still lists its processes", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(api, "getSessionTree").mockResolvedValue(tree());
    vi.spyOn(api, "listAlerts").mockRejectedValue(new Error("boom"));
    renderView();
    expect(await screen.findByText("This session's alerts could not be loaded, so none are shown.")).toBeVisible();
    expect(screen.getByText("ls /private/tmp | head -3")).toBeVisible();
  });

  it("names the agent from the processes when the session's record cannot be read", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(api, "getSessionTree").mockResolvedValue(tree());
    vi.spyOn(api, "listAgentSessions").mockRejectedValue(new Error("boom"));
    renderView();
    expect(await screen.findByRole("heading", { name: "claude-code 2.1.296" })).toBeVisible();
    expect(screen.getByText("/private/tmp")).toBeVisible();
  });

  it("says how many processes there are at least when the server capped its count", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(api, "getSessionTree").mockResolvedValue(tree({ truncated: true, total_matched: 10_000, total_matched_capped: true }));
    renderView();
    expect(await screen.findByRole("status")).toHaveTextContent("Showing the first 16 of more than 10,000 processes in this session.");
  });

  it("does not show the previous session's processes while the next session loads", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(api, "getSessionTree")
      .mockResolvedValueOnce(tree())
      .mockImplementationOnce(() => new Promise(() => undefined));
    render(
      <MemoryRouter initialEntries={[`/sessions/H1/${SESSION}`]}>
        <Link to="/sessions/H1/other-session">next</Link>
        <Routes>
          <Route path="/sessions/:hostId/:sessionId" element={<SessionView />} />
        </Routes>
      </MemoryRouter>,
    );
    expect(await screen.findByText("ls /private/tmp | head -3")).toBeVisible();
    fireEvent.click(screen.getByRole("link", { name: "next" }));
    expect(await screen.findByText("Loading...")).toBeVisible();
    expect(screen.queryByText("ls /private/tmp | head -3")).not.toBeInTheDocument();
  });

  it("does not show the previous session's error after moving to another session", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    let resolveNext: (t: TreeResponse) => void = () => undefined;
    vi.spyOn(api, "getSessionTree")
      .mockRejectedValueOnce(new Error("boom"))
      .mockImplementationOnce(() => new Promise((resolve) => { resolveNext = resolve; }));
    render(
      <MemoryRouter initialEntries={[`/sessions/H1/${SESSION}`]}>
        <Link to="/sessions/H1/other-session">next</Link>
        <Routes>
          <Route path="/sessions/:hostId/:sessionId" element={<SessionView />} />
        </Routes>
      </MemoryRouter>,
    );
    expect(await screen.findByText("Error: boom")).toBeVisible();
    fireEvent.click(screen.getByRole("link", { name: "next" }));
    expect(await screen.findByText("Loading...")).toBeVisible();
    expect(screen.queryByText("Error: boom")).not.toBeInTheDocument();
    resolveNext(tree());
    expect(await screen.findByText("ls /private/tmp | head -3")).toBeVisible();
  });

  it.each([
    [
      "an empty session",
      () => Promise.resolve(tree({ roots: [], returned: 0, total_matched: 0 })),
      "No processes are recorded for this session.",
    ],
    ["a failed read", () => Promise.reject(new Error("boom")), "Error: boom"],
  ])("states %s", async (_name, read, message) => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(api, "getSessionTree").mockImplementation(read);
    renderView();
    expect(await screen.findByText(message)).toBeVisible();
    expect(screen.getByRole("heading", { name: "Agent session" })).toBeVisible();
  });
});
