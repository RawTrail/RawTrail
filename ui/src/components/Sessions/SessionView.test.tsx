import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, within } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router";

import { SessionView } from "./SessionView";
import * as api from "../../api";
import { realSession, SESSION } from "../../test/agentSession";
import type { TreeResponse } from "../../types";

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

afterEach(() => { vi.restoreAllMocks(); });

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
