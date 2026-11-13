import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor, within } from "@testing-library/react";

import { ActivityTabs } from "./ActivityTabs";
import * as api from "../../api";
import { firstPageOf, mockActivity, totalsOf, type ActivityFixture } from "./activityTestKit";
import { formatNs } from "../Search/format";

const S = 1_000_000_000;

// session is the rollup the edr-dev Claude Code session's activity read returned: two curls to example.com and one to fleetdm.com,
// and a dig, each resolving its name first.
function session(over: Partial<ActivityFixture> = {}): ActivityFixture {
  const row = (pid: number, pidversion: number, count: number, first: number, last = first) =>
    ({ pid, pidversion, count, first_ns: first * S, last_ns: last * S });
  // lookup is a name one process resolved: a DNS row lists that process as its one resolver.
  const lookup = (pid: number, pidversion: number, count: number, first: number, last = first) =>
    ({ ...row(pid, pidversion, count, first, last), resolvers: [{ pid, pidversion, first_ns: first * S }], process_count: 1 });
  return {
    files: { events: 0, distinct: 0, rows: [] },
    network: {
      events: 4,
      distinct: 3,
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
      rows: [
        { ...lookup(43017, 3552872, 2, 9, 10), query_name: "example.com", addresses: ["104.20.23.154", "172.66.147.243"] },
        { ...lookup(43023, 3552887, 2, 11), query_name: "fleetdm.com", addresses: ["104.21.82.73"] },
        { ...lookup(43027, 3552899, 1, 12), query_name: "qa1285.example.org", addresses: [] },
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
    const { totals: spy } = mockActivity(session());
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
    mockActivity(session());
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} />);
    fireEvent.click(await screen.findByRole("button", { name: "DNS 3" }));
    expect(screen.getByText("5 queries for 3 names")).toBeVisible();
    expect(screen.getByText("104.20.23.154, 172.66.147.243")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Files 0" }));
    expect(screen.getByText("0 file events on 0 paths")).toBeVisible();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  // spec:web-ui/activity-times-read-as-an-age/first-and-last-read-as-an-age-with-the-exact-time-on-hover
  it("shows when a row was first and last seen as an age, with the exact time on hover", async () => {
    // Six minutes after the example.com lookup's first answer.
    vi.spyOn(Date, "now").mockReturnValue((9 * S) / 1_000_000 + 6 * 60_000);
    mockActivity(session());
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} />);
    await screen.findByText("4 connections to 3 destinations");
    // A connection seen once has no last.
    const connection = within(screen.getAllByRole("row")[1]).getAllByRole("cell");
    expect(connection[3]).toHaveTextContent("5m ago");
    expect(connection[3]).toHaveAttribute("title", formatNs(10 * S));
    expect(connection[4]).toHaveTextContent("");
    expect(connection[4]).not.toHaveAttribute("title");

    fireEvent.click(screen.getByRole("button", { name: "DNS 3" }));
    const lookup = within(screen.getAllByRole("row")[1]).getAllByRole("cell");
    expect(lookup[4]).toHaveTextContent("6m ago");
    expect(lookup[4]).toHaveAttribute("title", formatNs(9 * S));
    expect(lookup[5]).toHaveTextContent("5m ago");
    expect(lookup[5]).toHaveAttribute("title", formatNs(10 * S));
  });

  // spec:web-ui/activity-cells-keep-one-line-and-cut-paths-in-the-middle/a-long-answer-list-wraps-within-its-cap
  it("wraps a DNS row's answers within their cap", async () => {
    mockActivity(session());
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} />);
    fireEvent.click(await screen.findByRole("button", { name: "DNS 3" }));
    expect(screen.getByText("104.20.23.154, 172.66.147.243")).toHaveClass("activity-tabs__answers");
  });

  // spec:web-ui/the-dns-list-sorts-by-name/the-name-header-sorts-the-dns-list
  it("sorts the DNS list by name from its header, and back to first seen", async () => {
    const base = session();
    const late = { ...base.dns.rows[0], query_name: "a.example", first_ns: 20 * S, last_ns: 20 * S };
    mockActivity(session({ dns: { ...base.dns, rows: [...base.dns.rows, late] } }));
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} />);
    fireEvent.click(await screen.findByRole("button", { name: "DNS 3" }));
    const names = () => screen.getAllByRole("row").slice(1).map((r) => within(r).getAllByRole("cell")[0].textContent);
    expect(names()).toEqual(["example.com", "fleetdm.com", "qa1285.example.org", "a.example"]);

    const sort = screen.getByRole("button", { name: "Name" });
    expect(sort).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(sort);
    expect(names()).toEqual(["a.example", "example.com", "fleetdm.com", "qa1285.example.org"]);
    expect(screen.getByRole("columnheader", { name: "Name ▲" })).toHaveAttribute("aria-sort", "ascending");

    // The order outlives a visit to another tab.
    fireEvent.click(screen.getByRole("button", { name: "Network 3" }));
    fireEvent.click(screen.getByRole("button", { name: "DNS 3" }));
    expect(names()).toEqual(["a.example", "example.com", "fleetdm.com", "qa1285.example.org"]);

    fireEvent.click(screen.getByRole("button", { name: "Name ▲" }));
    expect(names()).toEqual(["example.com", "fleetdm.com", "qa1285.example.org", "a.example"]);
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
    mockActivity(session({ files }));
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "process", process: 7 }} />);
    fireEvent.click(await screen.findByRole("button", { name: "Files 2" }));
    const renameRow = screen.getByText("rename").closest("tr");
    expect(renameRow).not.toBeNull();
    expect(within(renameRow as HTMLElement).getAllByRole("cell")[1]).toHaveTextContent("/tmp/a → /tmp/b");
    expect(screen.getByText("rename")).toBeVisible();
    expect(screen.getByText("open")).toBeVisible();
  });

  // spec:web-ui/activity-cells-keep-one-line-and-cut-paths-in-the-middle/a-long-path-is-cut-in-its-middle-and-shown-whole-on-hover
  it("cuts each path in its middle, keeps its daemon tag in the row, and shows the whole path as its tooltip", async () => {
    const dir = "/Users/victor/.claude/plugins/cache/goland-claude-marketplace/modern-go-guidelines/1.0.0/.in_use";
    const sock = "/Users/victor/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock";
    const files = {
      events: 3, distinct: 3, truncated: false,
      rows: [
        { pid: 21989, pidversion: 38549, count: 1, first_ns: S, last_ns: S, operation: "create" as const, path: `${dir}/21989.tmp.c22cddba` },
        { pid: 21989, pidversion: 38549, count: 1, first_ns: 2 * S, last_ns: 2 * S, operation: "rename" as const,
          path: `${dir}/21989`, source_path: `${dir}/21989.tmp.c22cddba` },
        { pid: 21989, pidversion: 38549, count: 1, first_ns: 3 * S, last_ns: 3 * S, operation: "connect" as const, path: sock },
      ],
    };
    mockActivity(session({ files }));
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "process", process: 7 }} />);
    fireEvent.click(await screen.findByRole("button", { name: "Files 3" }));
    const [create, rename, connect] = screen.getAllByRole("row").slice(1).map((r) => within(r).getAllByRole("cell")[1]);
    // Everything a cell shows sits in its one row, so the paths shrink around a tag rather than wrap.
    for (const cell of [create, rename, connect]) {
      expect(cell.children).toHaveLength(1);
      expect(cell.firstElementChild).toHaveClass("activity-tabs__file");
    }
    const tooltips = (cell: HTMLElement) => Array.from(cell.querySelectorAll(".activity-tabs__path"), (p) => p.getAttribute("title"));
    expect(tooltips(create)).toEqual([`${dir}/21989.tmp.c22cddba`]);
    expect(tooltips(rename)).toEqual([`${dir}/21989.tmp.c22cddba`, `${dir}/21989`]);
    expect(tooltips(connect)).toEqual([sock]);
    // The head, where the file lives, is what the cell cuts; the tail, its parent directory and name, stays in view.
    const parts = (cell: HTMLElement) =>
      Array.from(cell.querySelectorAll(".activity-tabs__path"), (p) => [
        p.querySelector(".activity-tabs__path-head")?.textContent,
        p.querySelector(".activity-tabs__path-tail")?.textContent,
      ]);
    const head = "/Users/victor/.claude/plugins/cache/goland-claude-marketplace/modern-go-guidelines/1.0.0";
    expect(parts(create)).toEqual([[head, "/.in_use/21989.tmp.c22cddba"]]);
    expect(parts(rename)).toEqual([[head, "/.in_use/21989.tmp.c22cddba"], [head, "/.in_use/21989"]]);
    expect(parts(connect)).toEqual([["/Users/victor/Library/Group Containers/2BUA8C4S2C.com.1password", "/t/agent.sock"]]);
    expect(within(connect).getByText("1Password SSH agent")).toHaveClass("activity-tabs__daemon");
    // A copy of a cell keeps the whole path and spaces between its parts.
    expect(rename).toHaveTextContent(`${dir}/21989.tmp.c22cddba → ${dir}/21989`);
    expect(connect).toHaveTextContent(`${sock} 1Password SSH agent`);
  });

  // spec:web-ui/agent-file-activity-views/the-files-tab-lists-an-agent-s-edits
  // spec:web-ui/folded-agent-file-rows-name-their-process-count/a-fan-out-s-row-names-how-many-processes-it-counts
  it("lists an agent's file operations, and a rolled-up row as the directory it counts", async () => {
    const files = {
      events: 2020, distinct: 3, truncated: false,
      rows: [
        { pid: 4911, pidversion: 9, count: 4, first_ns: S, last_ns: 3 * S, operation: "modify" as const, path: "/r/main.go" },
        { pid: 5054, pidversion: 7, count: 1856, first_ns: 5 * S, last_ns: 6 * S, operation: "create" as const,
          path: "/r/node_modules", rolled_up: true },
        { pid: 2900, pidversion: 3, count: 160, first_ns: 7 * S, last_ns: 7 * S, operation: "modify" as const,
          path: "/tmp/b1.output", processes: 80 },
      ],
    };
    mockActivity(session({ files }));
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "process", process: 7 }} />);
    fireEvent.click(await screen.findByRole("button", { name: "Files 3" }));
    expect(screen.getByText("2,020 file events on 3 paths")).toBeVisible();
    const edit = screen.getByText("/r/main.go").closest("tr");
    expect(edit).not.toBeNull();
    expect(within(edit as HTMLElement).getByText("modify")).toBeVisible();
    expect(within(edit as HTMLElement).getByText("4")).toBeVisible();
    expect(within(edit as HTMLElement).queryByText("/ and below")).toBeNull();
    const install = screen.getByText("/ and below").closest("tr");
    expect(install).not.toBeNull();
    expect(within(install as HTMLElement).getByText("/r/node_modules", { exact: false })).toBeVisible();
    expect(within(install as HTMLElement).getByText("create")).toBeVisible();
    expect(within(install as HTMLElement).getByText("1,856")).toBeVisible();
    expect(within(install as HTMLElement).queryByText(/processes under/)).toBeNull();
    // A row folded from a fan-out names how many processes it counts, under the one they descend from.
    const fanout = screen.getByText("/tmp/b1.output").closest("tr");
    expect(fanout).not.toBeNull();
    expect(within(fanout as HTMLElement).getByText(/^up to 80 processes under/)).toBeVisible();
    expect(within(fanout as HTMLElement).getByText("160")).toBeVisible();
  });

  // A connect to a well-known daemon's socket names the daemon beside the path (issue #1355); an unknown socket and a file
  // operation on the same kind of path name none.
  // spec:web-ui/socket-daemon-names/a-connect-to-a-well-known-socket-names-its-daemon
  it("names the daemon behind a socket a connect reached", async () => {
    const files = {
      events: 4, distinct: 3, truncated: false,
      rows: [
        { pid: 3446, pidversion: 9, count: 1, first_ns: S, last_ns: S, operation: "connect" as const,
          path: "/private/tmp/com.apple.launchd.iIlTeYzViU/Listeners" },
        { pid: 3098, pidversion: 7, count: 2, first_ns: 2 * S, last_ns: 3 * S, operation: "connect" as const, path: "/private/tmp/qa1355.sock" },
        { pid: 3098, pidversion: 7, count: 1, first_ns: 4 * S, last_ns: 4 * S, operation: "delete" as const, path: "/var/run/docker.sock" },
      ],
    };
    mockActivity(session({ files }));
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "process", process: 7 }} />);
    fireEvent.click(await screen.findByRole("button", { name: "Files 3" }));
    const agent = screen.getByTitle("/private/tmp/com.apple.launchd.iIlTeYzViU/Listeners").closest("tr");
    expect(agent).not.toBeNull();
    expect(within(agent as HTMLElement).getByText("ssh-agent")).toBeVisible();
    expect(within(agent as HTMLElement).getByText("connect")).toBeVisible();
    const unknown = screen.getByTitle("/private/tmp/qa1355.sock").closest("tr");
    expect(unknown?.querySelector(".activity-tabs__daemon")).toBeNull();
    const removed = screen.getByTitle("/var/run/docker.sock").closest("tr");
    expect(within(removed as HTMLElement).queryByText("Docker")).toBeNull();
  });

  // spec:web-ui/a-dns-row-lists-every-process-that-resolved-its-name/a-name-several-processes-resolved-lists-each-of-them
  it("lists every process that resolved a name in its one row, and selects each at its own first lookup", async () => {
    const onProcess = vi.fn();
    const base = session();
    const shared = {
      ...base.dns.rows[0], query_name: "api.github.com", addresses: ["140.82.112.6"], count: 20, process_count: 18,
      resolvers: [
        { pid: 43019, pidversion: 3552877, first_ns: 3 * S },
        { pid: 43027, pidversion: 3552899, first_ns: 5 * S },
      ],
    };
    mockActivity(session({ dns: { ...base.dns, rows: [shared] } }));
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} onProcess={onProcess} />);
    fireEvent.click(await screen.findByRole("button", { name: "DNS 3" }));
    expect(screen.getAllByRole("row")).toHaveLength(2);
    const cell = within(screen.getAllByRole("row")[1]).getAllByRole("cell")[2];
    expect(cell).toHaveTextContent("curl (43019), dig (43027), and 16 others");
    expect(cell.firstElementChild).toHaveClass("activity-tabs__resolvers");
    fireEvent.click(within(cell).getByRole("button", { name: "dig (43027)" }));
    expect(onProcess).toHaveBeenCalledWith(expect.objectContaining({ id: 10, pid: 43027 }), 5 * S);
  });

  it("names each process of an older replica's DNS rows, which list a name once per process", async () => {
    const onProcess = vi.fn();
    const base = session();
    // An older replica sends no resolvers or process_count: each row is one process's, named by its pid and pidversion.
    const older = (pid: number, pidversion: number, first: number) =>
      ({ pid, pidversion, count: 1, first_ns: first * S, last_ns: first * S, query_name: "api.github.com", addresses: ["140.82.112.6"] });
    const rows = [older(43019, 3552877, 3), older(43027, 3552899, 5)];
    mockActivity(session({ dns: { ...base.dns, rows } }));
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} onProcess={onProcess} />);
    fireEvent.click(await screen.findByRole("button", { name: "DNS 3" }));
    const [, curl, dig] = screen.getAllByRole("row");
    expect(within(curl).getAllByRole("cell")[2]).toHaveTextContent(/^curl \(43019\)$/);
    expect(within(dig).getAllByRole("cell")[2]).toHaveTextContent(/^dig \(43027\)$/);
    fireEvent.click(within(dig).getByRole("button", { name: "dig (43027)" }));
    expect(onProcess).toHaveBeenCalledWith(expect.objectContaining({ id: 10, pid: 43027 }), 5 * S);
  });

  it("names a process by pid when the scope did not list it, and selects one when asked", async () => {
    const onProcess = vi.fn();
    const activity = session({ processes: session().processes.slice(1) });
    mockActivity(activity);
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} onProcess={onProcess} />);
    expect(await screen.findByText("pid 43017")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "dig (43027)" }));
    // The process and its row's first event, which together name the process to read when the view has not loaded it.
    expect(onProcess).toHaveBeenCalledWith(expect.objectContaining({ id: 10, pid: 43027 }), expect.any(Number));
  });

  it("lists an older replica's page, which names no processes, naming each row's process by pid", async () => {
    const { page } = mockActivity(session());
    // An older replica's page has rows and a cursor but no processes.
    page.mockImplementation((_h, _scope, cls, cursor) =>
      Promise.resolve(cursor === "" ? { rows: firstPageOf(session(), cls).rows } : { rows: [] }));
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} />);
    expect(await screen.findAllByText("pid 43017")).not.toHaveLength(0);
    expect(screen.queryByText("curl (43017)")).not.toBeInTheDocument();
  });

  it("names the generation a row belongs to, not another process that reused its pid", async () => {
    // pid 43017 is reused: generation 3552872 was curl, a later one a shell. The row belongs to curl.
    const activity = session({ processes: [{ id: 99, pid: 43017, pidversion: 1, path: "/bin/zsh" }, ...session().processes] });
    mockActivity(activity);
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} />);
    expect(await screen.findByText("curl (43017)")).toBeVisible();
    expect(screen.queryByText("zsh (43017)")).not.toBeInTheDocument();
  });

  it("says when a list was cut", async () => {
    mockActivity(session({ network: { ...session().network, next_cursor: "n1" } }));
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
    mockActivity(session({ scope }));
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} />);
    expect(await screen.findByText(message)).toBeVisible();
  });

  it("says nothing about a scope it reflects in full", async () => {
    mockActivity(session());
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} />);
    await screen.findByText("4 connections to 3 destinations");
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("reports totals that could not be read, and still lists what was", async () => {
    mockActivity(session());
    vi.spyOn(api, "getActivityTotals").mockRejectedValue(new Error("boom"));
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} />);
    expect(await screen.findByText("The totals could not be loaded: boom")).toBeVisible();
    expect(await screen.findByText("dig (43027)")).toBeVisible();
    expect(screen.getByText("Counting...")).toBeVisible();
  });

  it("rereads when the scope changes, and never shows the previous scope's activity meanwhile", async () => {
    const { totals, page } = mockActivity(session());
    totals.mockImplementation((_h, scope) =>
      scope.kind === "process" ? Promise.resolve(totalsOf(session())) : new Promise(() => undefined));
    page.mockImplementation((_h, scope, cls) =>
      scope.kind === "process" ? Promise.resolve(firstPageOf(session(), cls)) : new Promise(() => undefined));
    const { rerender } = render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "process", process: 7 }} />);
    await screen.findByText("4 connections to 3 destinations");
    rerender(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "subtree", process: 7 }} />);
    expect(await screen.findByText("Loading activity...")).toBeVisible();
    expect(screen.queryByText("4 connections to 3 destinations")).not.toBeInTheDocument();
  });
});

// A list reads its next page on request (issue #1424), from the cursor its first page named and then each page's own.
describe("ActivityTabs paging", () => {
  const S = 1_000_000_000;
  const dnsRow = (name: string, first: number) =>
    ({
      pid: 43027, pidversion: 3552899, count: 1, first_ns: first * S, last_ns: first * S, query_name: name, addresses: [],
      resolvers: [{ pid: 43027, pidversion: 3552899, first_ns: first * S }], process_count: 1,
    });
  const none = { rows: [], processes: [] };

  // spec:web-ui/agent-session-view-pages-its-processes-and-activity/the-rest-of-an-activity-list-comes-on-request
  it("reads a cut list's rows page by page until the last", async () => {
    const fixture = session({ dns: { events: 3, distinct: 3, rows: [dnsRow("a.example", 1)], next_cursor: "c1" } });
    const { page } = mockActivity(fixture);
    page.mockImplementation((_h, _s, cls, cursor) => Promise.resolve(cursor === "" ? firstPageOf(fixture, cls) : cursor === "c1"
      ? { rows: [dnsRow("b.example", 2)], next_cursor: "c2", processes: [] }
      : { rows: [dnsRow("c.example", 3)], processes: [] }));
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} />);
    await screen.findByText("4 connections to 3 destinations");
    fireEvent.click(screen.getByRole("button", { name: /^DNS/ }));
    expect(screen.getByRole("status")).toHaveTextContent("Showing the first 1 row");
    fireEvent.click(screen.getByRole("button", { name: "Load more rows" }));
    expect(await screen.findByText("b.example")).toBeVisible();
    expect(page).toHaveBeenLastCalledWith("H1", { kind: "session", session: "s1" }, "dns", "c1", "");
    fireEvent.click(screen.getByRole("button", { name: "Load more rows" }));
    expect(await screen.findByText("c.example")).toBeVisible();
    expect(page).toHaveBeenLastCalledWith("H1", { kind: "session", session: "s1" }, "dns", "c2", "");
    expect(screen.getByText("a.example")).toBeVisible();
    expect(screen.getByText("b.example")).toBeVisible();
    expect(screen.queryByRole("button", { name: "Load more rows" })).toBeNull();
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("says when the next rows could not be loaded, and offers them again", async () => {
    const fixture = session({
      files: { events: 2, distinct: 2, next_cursor: "f1",
        rows: [{ pid: 43017, pidversion: 3552872, count: 1, first_ns: S, last_ns: S, operation: "modify", path: "/r/a" }] },
    });
    const { page } = mockActivity(fixture);
    page.mockImplementation((_h, _s, cls, cursor) =>
      cursor === "" ? Promise.resolve(firstPageOf(fixture, cls)) : Promise.reject(new Error("down")));
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} />);
    await screen.findByText("4 connections to 3 destinations");
    fireEvent.click(screen.getByRole("button", { name: /^Files/ }));
    fireEvent.click(screen.getByRole("button", { name: "Load more rows" }));
    expect(await screen.findByText("The next rows could not be loaded.")).toBeVisible();
    expect(screen.getByRole("button", { name: "Load more rows" })).toBeVisible();
  });

  // spec:web-ui/the-session-view-searches-its-processes-and-activity-on-the-server/an-activity-search-reads-the-class-from-the-server
  it("lists each class's rows the server finds for a search, and pages them", async () => {
    const { page } = mockActivity(session({ dns: { events: 3, distinct: 3, rows: [dnsRow("a.example", 1), dnsRow("ab.example", 2)] } }));
    page.mockImplementation((_h, _s, cls, cursor) => Promise.resolve(cls !== "dns"
      ? none
      : cursor === "" ? { rows: [dnsRow("b.example", 3)], next_cursor: "q2", processes: [] } : { rows: [dnsRow("bb.example", 4)], processes: [] }));
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} search="b." />);
    for (const cls of ["network", "dns", "files"]) {
      await waitFor(() => { expect(page).toHaveBeenCalledWith("H1", { kind: "session", session: "s1" }, cls, "", "b."); });
    }
    fireEvent.click(await screen.findByRole("button", { name: /^DNS/ }));
    expect(await screen.findByText("b.example")).toBeVisible();
    expect(screen.queryByText("a.example")).toBeNull();
    expect(screen.getByRole("button", { name: /^DNS/ })).toHaveTextContent("3", { normalizeWhitespace: true });
    fireEvent.click(screen.getByRole("button", { name: "Load more rows" }));
    expect(await screen.findByText("bb.example")).toBeVisible();
    expect(page).toHaveBeenLastCalledWith("H1", { kind: "session", session: "s1" }, "dns", "q2", "b.");
  });

  it("offers a failed first page again", async () => {
    const { page } = mockActivity(session({ dns: { events: 1, distinct: 1, rows: [] } }));
    let dnsAttempts = 0;
    page.mockImplementation((_h, _s, cls) => {
      if (cls !== "dns") return Promise.resolve(none);
      dnsAttempts++;
      return dnsAttempts === 1 ? Promise.reject(new Error("down")) : Promise.resolve({ rows: [dnsRow("b.example", 3)], processes: [] });
    });
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} search="b." />);
    fireEvent.click(await screen.findByRole("button", { name: /^DNS/ }));
    expect(await screen.findByText("The list could not be loaded.")).toBeVisible();
    expect(screen.queryByRole("button", { name: "Load more rows" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("b.example")).toBeVisible();
    expect(dnsAttempts).toBe(2);
  });

  it("drops a page read for a scope the operator has since left", async () => {
    const fixture = (scope: api.ActivityScopeQuery) => session({
      dns: { events: 2, distinct: 2, rows: [dnsRow(`${scope.kind === "session" ? scope.session : ""}-a.example`, 1)], next_cursor: "c1" },
    });
    let landOld: (page: { rows: unknown[]; processes: [] }) => void = () => undefined;
    const { page } = mockActivity(fixture);
    page.mockImplementation((_h, scope, cls, cursor) => {
      if (cursor === "") return Promise.resolve(firstPageOf(fixture(scope), cls));
      return scope.kind === "session" && scope.session === "s1"
        ? new Promise((resolve) => { landOld = resolve; })
        : Promise.resolve({ rows: [dnsRow("s2-b.example", 2)], processes: [] });
    });
    const { rerender } = render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} />);
    fireEvent.click(await screen.findByRole("button", { name: /^DNS/ }));
    fireEvent.click(await screen.findByRole("button", { name: "Load more rows" }));
    rerender(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s2" }} />);
    expect(await screen.findByText("s2-a.example")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Load more rows" }));
    expect(await screen.findByText("s2-b.example")).toBeVisible();
    landOld({ rows: [dnsRow("s1-b.example", 2)], processes: [] });
    await new Promise((resolve) => { setTimeout(resolve, 0); });
    expect(screen.getByText("s2-b.example")).toBeVisible();
    expect(screen.queryByText("s1-b.example")).toBeNull();
  });

  it("drops a page of an earlier read of a scope the operator left and came back to", async () => {
    const fixture = (scope: api.ActivityScopeQuery) => session({
      dns: { events: 1, distinct: 1, rows: [dnsRow(`${scope.kind === "session" ? scope.session : ""}-a.example`, 1)] },
    });
    // The first read of s1's DNS page is held; the second answers at once.
    const held: (() => void)[] = [];
    const { page } = mockActivity(fixture);
    page.mockImplementation((_h, scope, cls) => {
      const answer = firstPageOf(fixture(scope), cls);
      if (cls !== "dns" || scope.kind !== "session" || scope.session !== "s1" || held.length > 0) return Promise.resolve(answer);
      return new Promise((resolve) => { held.push(() => { resolve({ ...answer, next_cursor: "stale" }); }); });
    });
    const at = (session: string) => <ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session }} />;
    const { rerender } = render(at("s1"));
    rerender(at("s2"));
    fireEvent.click(await screen.findByRole("button", { name: /^DNS/ }));
    expect(await screen.findByText("s2-a.example")).toBeVisible();
    rerender(at("s1"));
    expect(await screen.findByText("s1-a.example")).toBeVisible();
    held[0]();
    await new Promise((resolve) => { setTimeout(resolve, 0); });
    expect(screen.getAllByText("s1-a.example")).toHaveLength(1);
    expect(screen.queryByRole("button", { name: "Load more rows" })).toBeNull();
  });
});

// The totals and each class's list are read at once, and each is shown as it arrives (issue #1550).
describe("ActivityTabs loading in parts", () => {
  // spec:web-ui/activity-lists-show-as-they-arrive/a-list-shows-before-the-totals-and-the-other-lists
  it("shows the first list to arrive before the totals and the other lists", async () => {
    const fixture = session();
    const { totals, page } = mockActivity(fixture);
    totals.mockImplementation(() => new Promise(() => undefined));
    page.mockImplementation((_h, _s, cls) => (cls === "dns" ? Promise.resolve(firstPageOf(fixture, cls)) : new Promise(() => undefined)));
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} />);
    expect(await screen.findByText("fleetdm.com")).toBeVisible();
    expect(screen.getByRole("button", { name: "DNS" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByText("Counting...")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Network" }));
    expect(screen.getByText("Loading activity...")).toBeVisible();
  });

  it("passes over a list that arrived empty for the first that has rows", async () => {
    const fixture = session({ network: { events: 0, distinct: 0, rows: [] } });
    const { page } = mockActivity(fixture);
    let landDNS: (p: ReturnType<typeof firstPageOf>) => void = () => undefined;
    page.mockImplementation((_h, _s, cls) => (cls === "dns"
      ? new Promise((resolve) => { landDNS = resolve; })
      : Promise.resolve(firstPageOf(fixture, cls))));
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} />);
    // Network and Files arrive empty: the first of them is shown until a list with rows arrives.
    expect(await screen.findByRole("button", { name: "Network 0" })).toHaveAttribute("aria-pressed", "true");
    landDNS(firstPageOf(fixture, "dns"));
    expect(await screen.findByText("fleetdm.com")).toBeVisible();
    expect(screen.getByRole("button", { name: "DNS 3" })).toHaveAttribute("aria-pressed", "true");
  });

  it("keeps the class the operator picked while another list arrives first", async () => {
    const fixture = session();
    const { page } = mockActivity(fixture);
    let landFiles: (p: ReturnType<typeof firstPageOf>) => void = () => undefined;
    page.mockImplementation((_h, _s, cls) => (cls === "files"
      ? new Promise((resolve) => { landFiles = resolve; })
      : Promise.resolve(firstPageOf(fixture, cls))));
    render(<ActivityTabs label="Session activity" hostId="H1" scope={{ kind: "session", session: "s1" }} />);
    fireEvent.click(screen.getByRole("button", { name: "Files" }));
    expect(await screen.findByRole("button", { name: "Files 0" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByText("Loading activity...")).toBeVisible();
    landFiles(firstPageOf(fixture, "files"));
    expect(await screen.findByText("0 file events on 0 paths")).toBeVisible();
  });
});
