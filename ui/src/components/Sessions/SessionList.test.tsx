import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router";

import { SessionList } from "./SessionList";
import { PermissionAction, PermissionsContext } from "../../permissions-core";
import { ProfileContext, type Profile } from "../../profile-core";
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
  // spec:web-ui/agent-sessions-list/sessions-are-listed-with-their-project-and-activity
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

  // spec:web-ui/agent-sessions-list/a-session-opens-its-activity
  it("opens the session's own view", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(api, "listAgentSessions").mockResolvedValue({ rows: [session({ host_id: "H 1" })], total_matched: 1 });
    renderList();
    const link = await screen.findByRole("link", { name: "claude-code" });
    expect(link).toHaveAttribute("href", "/agent-sessions/H%201/169b5bb1-29b5-41da-9a54-92cd72599ad9");
    expect(screen.getByText("H 1")).toBeVisible();
  });

  // spec:web-ui/agent-sessions-list/the-server-pages-the-list
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

  // spec:web-ui/sessions-list-bypass-badge/a-bypassed-session-s-row-is-badged
  it("badges the sessions that ran bypassed", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    const spy = vi.spyOn(api, "listAgentSessions").mockResolvedValue({
      rows: [session({ bypass: true }), session({ agent_session_id: "s2", agent_name: "codex" })], total_matched: 2,
    });
    renderList();
    await waitFor(() => { expect(spy).toHaveBeenCalled(); });
    const bypassed = (await screen.findByRole("link", { name: "claude-code" })).closest("tr");
    const badge = screen.getByText("Bypass permissions");
    expect(badge).toBeVisible();
    expect(bypassed).toContainElement(badge);
    expect(badge.parentElement).toHaveAttribute("title", "Ran in bypass mode, or was forked from a session that did");
    expect(screen.getAllByText("Bypass permissions")).toHaveLength(1);
  });

  // spec:web-ui/agent-sessions-list/sessions-are-listed-with-their-project-and-activity
  it("says how long ago a session was last active and how long it ran, with the exact times on hover", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    const hour = 3_600_000;
    vi.spyOn(Date, "now").mockReturnValue(100 * hour);
    const first = (100 * hour - 3 * 24 * hour - 2 * hour - 14 * 60_000) * 1_000_000;
    const last = (100 * hour - 3 * 24 * hour) * 1_000_000;
    vi.spyOn(api, "listAgentSessions").mockResolvedValue({ rows: [session({ first_seen_ns: first, last_seen_ns: last })], total_matched: 1 });
    renderList();
    const row = (await screen.findByRole("link", { name: "claude-code" })).closest("tr");
    const [active, duration] = [row?.querySelectorAll("td")[3], row?.querySelectorAll("td")[4]];
    expect(active).toHaveTextContent(/^3d ago$/);
    expect(active).toHaveAttribute("title", new Date(last / 1_000_000).toLocaleString());
    expect(duration).toHaveTextContent(/^2h 14m$/);
    expect(duration).toHaveAttribute("title",
      `${new Date(first / 1_000_000).toLocaleString()} to ${new Date(last / 1_000_000).toLocaleString()}`);
    // A time never breaks across lines.
    for (const cell of [active, duration]) expect(cell).toHaveClass("search-page__nowrap");
  });

  // spec:web-ui/agent-sessions-list/a-capped-process-count-says-there-are-more
  it("marks a process count the server stopped at its bound, and leaves an exact one plain", async () => {
    vi.spyOn(api, "listHosts").mockResolvedValue([]);
    vi.spyOn(api, "listAgentSessions").mockResolvedValue({
      rows: [
        session({ process_count: 10_000, process_count_capped: true }),
        session({ agent_session_id: "8d9e0f1a-2b3c-4d5e-8f6a-7b8c9d0e1f2a", process_count: 9_999 }),
      ],
      total_matched: 2,
    });
    renderList();
    const rows = (await screen.findAllByRole("link", { name: "claude-code" })).map((link) => link.closest("tr"));
    const capped = rows[0]?.querySelectorAll("td")[5];
    expect(capped?.textContent).toBe(`${(10_000).toLocaleString()}+`);
    expect(capped).toHaveAttribute("title", `More than ${(10_000).toLocaleString()} processes`);
    const exact = rows[1]?.querySelectorAll("td")[5];
    expect(exact?.textContent).toBe((9_999).toLocaleString());
    expect(exact).not.toHaveAttribute("title");
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

  describe("on a laptop with no session yet", () => {
    function renderOn(profile: Profile, perms: string[], initial = "/agent-sessions") {
      return render(
        <MemoryRouter initialEntries={[initial]}>
          <PermissionsContext.Provider value={perms}>
            <ProfileContext.Provider value={{ profile, showAdvanced: true, setShowAdvanced: vi.fn() }}>
              <Routes>
                <Route path="/agent-sessions" element={<SessionList />} />
              </Routes>
            </ProfileContext.Provider>
          </PermissionsContext.Provider>
        </MemoryRouter>,
      );
    }
    const perms = [PermissionAction.ProcessRead, PermissionAction.HostRead];

    it("points the person at the setup checklist", async () => {
      vi.spyOn(api, "listHosts").mockResolvedValue([]);
      vi.spyOn(api, "listAgentSessions").mockResolvedValue({ rows: [], total_matched: 0 });
      renderOn("laptop", perms);
      expect(await screen.findByRole("link", { name: "Set up this Mac" })).toHaveAttribute("href", "/setup");
    });

    it("does not point there on a server, without host access, or once a session exists", async () => {
      vi.spyOn(api, "listHosts").mockResolvedValue([]);
      const sessions = vi.spyOn(api, "listAgentSessions").mockResolvedValue({ rows: [], total_matched: 0 });
      const cases: [Profile, string[], string][] = [
        ["server", perms, "/agent-sessions"],
        ["laptop", [PermissionAction.ProcessRead], "/agent-sessions"],
      ];
      for (const [profile, granted, at] of cases) {
        const { unmount } = renderOn(profile, granted, at);
        expect(await screen.findByText("No AI agent sessions match.")).toBeVisible();
        expect(screen.queryByRole("link", { name: "Set up this Mac" })).toBeNull();
        unmount();
      }
      sessions.mockResolvedValue({ rows: [session()], total_matched: 1 });
      renderOn("laptop", perms);
      expect(await screen.findByText("claude-code")).toBeVisible();
      expect(screen.queryByRole("link", { name: "Set up this Mac" })).toBeNull();
    });
  });
});
