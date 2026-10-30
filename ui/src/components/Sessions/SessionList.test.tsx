import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router";

import { SessionList } from "./SessionList";
import { PermissionsContext } from "../../permissions-core";
import * as api from "../../api";
import type { AgentSessionSummary } from "../../types";

function session(over: Partial<AgentSessionSummary> = {}): AgentSessionSummary {
  return {
    host_id: "H1",
    agent_session_id: "169b5bb1-29b5-41da-9a54-92cd72599ad9",
    agent_name: "claude-code",
    agent_version: "2.1.296",
    cwd: "/private/tmp",
    first_seen_ns: 1_000_000_000,
    last_seen_ns: 9_000_000_000,
    process_count: 16,
    alert_count: 1,
    bypass: false,
    ...over,
  };
}

function renderList(initial = "/agent-sessions") {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Routes>
        <Route path="/agent-sessions" element={<SessionList />} />
      </Routes>
    </MemoryRouter>,
  );
}

afterEach(() => { vi.restoreAllMocks(); });

describe("SessionList", () => {
  // spec:web-ui/ai-agent-sessions-page/sessions-are-listed-with-their-project-and-activity
  it("lists a session with its agent, project, host name and counts", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([{ host_id: "H1", hostname: "mac-a.local" } as never]);
    vi.spyOn(api, "listAgentSessions").mockResolvedValue({ rows: [session()], total_matched: 1 });
    renderList();
    const row = (await screen.findByRole("link", { name: "claude-code" })).closest("tr");
    expect(row).not.toBeNull();
    const cells = Array.from(row?.querySelectorAll("td") ?? []).map((td) => td.textContent);
    expect(cells[0]).toBe("claude-code 2.1.296");
    expect(cells[1]).toBe("/private/tmp");
    expect(await screen.findByText("mac-a.local")).toBeVisible();
    expect(cells.slice(5, 8)).toEqual(["16", "1", ""]);
    expect(cells[8]).toBe("169b5bb1");
    expect(screen.getByText("Showing 1 of 1 matches")).toBeVisible();
  });

  // spec:web-ui/ai-agent-sessions-page/a-session-opens-its-activity
  it("opens the session's own view", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(api, "listAgentSessions").mockResolvedValue({ rows: [session({ host_id: "H 1" })], total_matched: 1 });
    renderList();
    const link = await screen.findByRole("link", { name: "claude-code" });
    expect(link).toHaveAttribute("href", "/agent-sessions/H%201/169b5bb1-29b5-41da-9a54-92cd72599ad9");
    expect(screen.getByText("H 1")).toBeVisible();
  });

  // spec:web-ui/ai-agent-sessions-page/filters-and-paging-are-applied-by-the-server
  it("passes the URL filters to the server and resolves the active window against the clock", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(Date, "now").mockReturnValue(10 * 24 * 3_600_000);
    const spy = vi.spyOn(api, "listAgentSessions").mockResolvedValue({ rows: [session()], total_matched: 1 });
    renderList("/agent-sessions?host_id=H1&agent=claude-code&active=7d");
    await waitFor(() => { expect(spy).toHaveBeenCalled(); });
    expect(spy.mock.calls[0][0]).toEqual({ host_id: "H1", agent: "claude-code", from: String(3 * 24 * 3_600_000 * 1_000_000) });
    expect(spy.mock.calls[0][1]).toBeUndefined();
  });

  // spec:web-ui/ai-agent-sessions-page/filters-and-paging-are-applied-by-the-server
  it("keeps the first page's window for every later page", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    const now = vi.spyOn(Date, "now").mockReturnValue(10 * 24 * 3_600_000);
    const spy = vi.spyOn(api, "listAgentSessions")
      .mockResolvedValueOnce({ rows: [session()], next_cursor: "page-2", total_matched: 2 })
      .mockResolvedValueOnce({ rows: [session({ agent_session_id: "s2", agent_name: "codex" })], total_matched: 2 });
    renderList("/agent-sessions?active=1d");
    const loadMore = await screen.findByRole("button", { name: "Load more" });
    now.mockReturnValue(11 * 24 * 3_600_000);
    fireEvent.click(loadMore);
    expect(await screen.findByRole("link", { name: "codex" })).toBeVisible();
    expect(spy.mock.calls[1]).toEqual([{ from: String(9 * 24 * 3_600_000 * 1_000_000) }, "page-2"]);
  });

  it("removing a filter refetches without it", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    const spy = vi.spyOn(api, "listAgentSessions").mockResolvedValue({ rows: [session()], total_matched: 1 });
    renderList("/agent-sessions?agent=claude-code&active=1d");
    await waitFor(() => { expect(spy).toHaveBeenCalled(); });
    fireEvent.click(screen.getByRole("button", { name: "Remove Agent filter" }));
    await waitFor(() => {
      expect(spy.mock.calls.some(([f]) => f.agent === undefined && f.from !== undefined)).toBe(true);
    });
  });

  // spec:web-ui/ai-agent-sessions-page/filters-and-paging-are-applied-by-the-server
  it("loads the next page with the server's cursor and appends it", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    const spy = vi.spyOn(api, "listAgentSessions")
      .mockResolvedValueOnce({ rows: [session()], next_cursor: "page-2", total_matched: 2 })
      .mockResolvedValueOnce({ rows: [session({ agent_session_id: "s2", agent_name: "codex" })], total_matched: 2 });
    renderList();
    fireEvent.click(await screen.findByRole("button", { name: "Load more" }));
    expect(await screen.findByRole("link", { name: "codex" })).toBeVisible();
    expect(screen.getByRole("link", { name: "claude-code" })).toBeVisible();
    expect(spy.mock.calls[1][1]).toBe("page-2");
    expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
  });

  // spec:web-ui/sessions-list-bypass-filter/the-list-narrows-to-sessions-that-ran-bypassed
  it("asks the server for the sessions that ran bypassed and badges them", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    const spy = vi.spyOn(api, "listAgentSessions").mockResolvedValue({
      rows: [session({ bypass: true }), session({ agent_session_id: "s2", agent_name: "codex" })], total_matched: 2,
    });
    renderList("/agent-sessions?bypass=true");
    await waitFor(() => { expect(spy).toHaveBeenCalled(); });
    expect(spy.mock.calls[0][0]).toEqual({ bypass: "true" });
    expect(screen.getByText("Permissions: Ran in bypass mode (or forked from a session that did)")).toBeVisible();
    const bypassed = (await screen.findByRole("link", { name: "claude-code" })).closest("tr");
    const badge = screen.getByText("Bypass permissions");
    expect(badge).toBeVisible();
    expect(bypassed).toContainElement(badge);
    expect(badge.parentElement).toHaveAttribute("title", "Ran in bypass mode, or was forked from a session that did");
    expect(screen.getAllByText("Bypass permissions")).toHaveLength(1);
  });

  // spec:web-ui/sessions-list-tokens/a-session-lists-its-tokens
  it("lists a session's tokens compactly, with the exact count on hover", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(api, "listAgentSessions").mockResolvedValue({ rows: [session({ tokens: 1_234_567 })], total_matched: 1 });
    renderList();
    const row = (await screen.findByRole("link", { name: "claude-code" })).closest("tr");
    const cell = row?.querySelectorAll("td")[7];
    expect(cell).toHaveTextContent(/^1\.2M$/);
    expect(cell).toHaveAttribute("title", `${(1_234_567).toLocaleString()} tokens`);
  });

  // spec:web-ui/agent-prompt-search/only-an-operator-who-may-read-prompts-can-search-them
  it("offers the prompt search only to an operator who may read prompts", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(api, "listAgentSessions").mockResolvedValue({ rows: [], total_matched: 0 });
    const page = (perms: string[]) => (
      <PermissionsContext.Provider value={perms}>
        <MemoryRouter initialEntries={["/agent-sessions"]}>
          <Routes>
            <Route path="/agent-sessions" element={<SessionList />} />
          </Routes>
        </MemoryRouter>
      </PermissionsContext.Provider>
    );
    const { rerender } = render(page(["process.read"]));
    expect(await screen.findByText("No AI agent sessions match.")).toBeVisible();
    expect(screen.queryByRole("region", { name: "Search prompts" })).not.toBeInTheDocument();
    rerender(page(["process.read", "agent_prompt.read"]));
    expect(screen.getByRole("region", { name: "Search prompts" })).toBeVisible();
  });

  it("says so when no session matches", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(api, "listAgentSessions").mockResolvedValue({ rows: [], total_matched: 0 });
    renderList();
    expect(await screen.findByText("No AI agent sessions match.")).toBeVisible();
  });
});
