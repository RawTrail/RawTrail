import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, within } from "@testing-library/react";

import { ActivityTabs } from "./ActivityTabs";
import * as api from "../../api";
import type { ActivityResponse } from "../../types";

const S = 1_000_000_000;

// session is the rollup the edr-dev Claude Code session's activity read returned: two curls to example.com and one to fleetdm.com,
// and a dig, each resolving its name first.
function session(over: Partial<ActivityResponse> = {}): ActivityResponse {
  const row = (pid: number, pidversion: number, count: number, first: number, last = first) =>
    ({ pid, pidversion, count, first_ns: first * S, last_ns: last * S });
  return {
    files: { events: 0, distinct: 0, truncated: false, rows: [] },
    network: {
      events: 4,
      distinct: 3,
      truncated: false,
      rows: [
        { ...row(43017, 3552872, 1, 10), remote_address: "172.66.147.243", remote_port: 443, protocol: "tcp" },
        { ...row(43019, 3552877, 1, 11), remote_address: "172.66.147.243", remote_port: 443, protocol: "tcp" },
        { ...row(43023, 3552887, 1, 12), remote_address: "104.21.82.73", remote_port: 443, protocol: "tcp" },
        { ...row(43027, 3552899, 1, 13), remote_address: "192.168.64.1", remote_port: 53, protocol: "udp", remote_hostname: "gateway" },
      ],
    },
    dns: {
      events: 5,
      distinct: 3,
      truncated: false,
      rows: [
        { ...row(43017, 3552872, 2, 9, 10), query_name: "example.com", addresses: ["104.20.23.154", "172.66.147.243"] },
        { ...row(43023, 3552887, 2, 11), query_name: "fleetdm.com", addresses: ["104.21.82.73"] },
        { ...row(43027, 3552899, 1, 12), query_name: "qa1285.example.org", addresses: [] },
      ],
    },
    scope: { applied: true, processes: 12, unmatched: 0, truncated: false },
    processes: [
      { id: 7, pid: 43017, pidversion: 3552872, path: "/usr/bin/curl" },
      { id: 8, pid: 43019, pidversion: 3552877, path: "/usr/bin/curl" },
      { id: 9, pid: 43023, pidversion: 3552887, path: "/usr/bin/curl" },
      { id: 10, pid: 43027, pidversion: 3552899, path: "/usr/bin/dig" },
    ],
    ...over,
  };
}

afterEach(() => { vi.restoreAllMocks(); });

describe("ActivityTabs", () => {
  // spec:web-ui/activity-tabs/a-session-s-network-tab-lists-every-destination-with-its-process
  it("lists every destination a session connected to, with the process that made each connection", async () => {
    const spy = vi.spyOn(api, "getActivity").mockResolvedValue(session());
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} />);
    expect(await screen.findByText("4 connections to 3 destinations")).toBeVisible();
    expect(spy).toHaveBeenCalledWith("H1", { kind: "session", session: "s1" });
    const rows = screen.getAllByRole("row").slice(1).map((r) => within(r).getAllByRole("cell").slice(0, 3).map((c) => c.textContent));
    expect(rows).toEqual([
      ["172.66.147.243:443 tcp", "curl (43017)", "1"],
      ["172.66.147.243:443 tcp", "curl (43019)", "1"],
      ["104.21.82.73:443 tcp", "curl (43023)", "1"],
      ["192.168.64.1:53 udp gateway", "dig (43027)", "1"],
    ]);
    // The tab labels count distinct artifacts.
    expect(screen.getByRole("button", { name: "Network 3" })).toHaveAttribute("aria-pressed", "true");
  });

  it("switches to the DNS and Files tabs", async () => {
    vi.spyOn(api, "getActivity").mockResolvedValue(session());
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} />);
    fireEvent.click(await screen.findByRole("button", { name: "DNS 3" }));
    expect(screen.getByText("5 queries for 3 names")).toBeVisible();
    expect(screen.getByText("104.20.23.154, 172.66.147.243")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Files 0" }));
    expect(screen.getByText("0 file events on 0 paths")).toBeVisible();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("shows a rename with its origin and labels each operation", async () => {
    const files = {
      events: 2, distinct: 2, truncated: false,
      rows: [
        { pid: 43017, pidversion: 3552872, count: 1, first_ns: S, last_ns: S, operation: "open" as const, path: "/tmp/a" },
        { pid: 43017, pidversion: 3552872, count: 1, first_ns: 2 * S, last_ns: 2 * S, operation: "file_rename" as const,
          path: "/tmp/b", source_path: "/tmp/a" },
      ],
    };
    vi.spyOn(api, "getActivity").mockResolvedValue(session({ files }));
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "process", process: 7 }} />);
    fireEvent.click(await screen.findByRole("button", { name: "Files 2" }));
    expect(screen.getByText("/tmp/a → /tmp/b")).toBeVisible();
    expect(screen.getByText("rename")).toBeVisible();
    expect(screen.getByText("open")).toBeVisible();
  });

  it("names a process by pid when the scope did not list it, and selects one when asked", async () => {
    const onProcess = vi.fn();
    const activity = session({ processes: session().processes.slice(1) });
    vi.spyOn(api, "getActivity").mockResolvedValue(activity);
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} onProcess={onProcess} />);
    expect(await screen.findByText("pid 43017")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "dig (43027)" }));
    expect(onProcess).toHaveBeenCalledWith(10);
  });

  it("names the generation a row belongs to, not another process that reused its pid", async () => {
    // pid 43017 is reused: generation 3552872 was curl, a later one a shell. The row belongs to curl.
    const activity = session({ processes: [{ id: 99, pid: 43017, pidversion: 1, path: "/bin/zsh" }, ...session().processes] });
    vi.spyOn(api, "getActivity").mockResolvedValue(activity);
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} />);
    expect(await screen.findByText("curl (43017)")).toBeVisible();
    expect(screen.queryByText("zsh (43017)")).not.toBeInTheDocument();
  });

  it("says when a list was cut", async () => {
    vi.spyOn(api, "getActivity").mockResolvedValue(session({ network: { ...session().network, truncated: true } }));
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "subtree", process: 7 }} />);
    expect(await screen.findByText("Showing the first 4 rows; there are more than the list holds.")).toBeVisible();
  });

  // spec:web-ui/activity-tabs/the-tabs-say-how-much-of-the-scope-they-reflect
  it.each([
    [
      // Truncated with nothing applied: the unread processes may carry generations, so only "partial" can be said.
      { applied: false, processes: 5000, unmatched: 5000, truncated: true },
      "This scope has more processes than are read, so its activity is partial.",
    ],
    [
      { applied: false, processes: 0, unmatched: 0, truncated: false },
      "No stored process matches this scope, so there is no activity to show.",
    ],
    [
      { applied: false, processes: 2, unmatched: 2, truncated: false },
      "None of this scope's processes carry a process generation, so their activity cannot be read.",
    ],
    [
      { applied: true, processes: 5000, unmatched: 1, truncated: true },
      "This scope has more processes than are read, so its activity is partial.",
    ],
    [
      { applied: true, processes: 3, unmatched: 1, truncated: false },
      "1 process carries no process generation, so their activity is not counted.",
    ],
    [
      { applied: true, processes: 3, unmatched: 2, truncated: false },
      "2 processes carry no process generation, so their activity is not counted.",
    ],
  ])("reports the scope %j", async (scope, message) => {
    vi.spyOn(api, "getActivity").mockResolvedValue(session({ scope }));
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} />);
    expect(await screen.findByText(message)).toBeVisible();
  });

  it("says nothing about a scope it reflects in full", async () => {
    vi.spyOn(api, "getActivity").mockResolvedValue(session());
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} />);
    await screen.findByText("4 connections to 3 destinations");
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("reports a failed read", async () => {
    vi.spyOn(api, "getActivity").mockRejectedValue(new Error("boom"));
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} />);
    expect(await screen.findByText("Error: boom")).toBeVisible();
  });

  it("rereads when the scope changes, and never shows the previous scope's activity meanwhile", async () => {
    vi.spyOn(api, "getActivity")
      .mockResolvedValueOnce(session())
      .mockImplementationOnce(() => new Promise(() => undefined));
    const { rerender } = render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "process", process: 7 }} />);
    await screen.findByText("4 connections to 3 destinations");
    rerender(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "subtree", process: 7 }} />);
    expect(await screen.findByText("Loading activity...")).toBeVisible();
    expect(screen.queryByText("4 connections to 3 destinations")).not.toBeInTheDocument();
  });
});
