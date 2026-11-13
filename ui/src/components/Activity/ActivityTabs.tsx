import { Fragment, useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import "./ActivityTabs.scss";
import { getActivityPage, getActivityTotals, type ActivityScopeQuery } from "../../api";
import type {
  ActivityPage, ActivityProcess, ActivityRow, ActivityTotals, DNSActivityRow, DNSResolver, FileActivityRow, NetworkActivityRow,
} from "../../types";
import { Button } from "../ui/Button";
import { Table, EmptyState } from "../ui/Table";
import { formatNs, executableName, hostPort, plural } from "../Search/format";
import { formatRelativeNs } from "../../time";
import { scopeNote } from "./activityScope";
import { splitPath } from "./pathParts";
import { socketDaemon } from "./socketDaemon";

type TabKey = "files" | "network" | "dns";

// OPERATION_LABELS names a watched-path event's type as an operation. An AI agent's file activity (issue #1287) already carries its
// operation (create, modify, rename, delete, or connect to a Unix-domain socket), which is shown as it is.
const OPERATION_LABELS: Record<string, string> = {
  open: "open",
  file_rename: "rename",
  file_truncate: "truncate",
  file_delete: "delete",
};

// DAEMON_TITLE explains a daemon's name beside a socket: the session asked it for work, and what the daemon then did is its own.
const DAEMON_TITLE = "The daemon this socket belongs to. The connect is the session's request; what the daemon did with it is not shown.";

// ROLLED_UP_TITLE explains a folded row: the extension sends at most a set number of rows a second, so a busy window's files are
// counted under their directory.
const ROLLED_UP_TITLE = "Too many files changed at once to list each: this row counts the files under this directory.";

// FOLDED_TITLE explains a row folded from several processes: past the same budget, the extension counts a fan-out's operations on one
// path under the process its jobs all descend from. The count is summed over each second's fold, so it is at most the processes.
const FOLDED_TITLE =
  "Too many processes did this at once to list each: this row counts their operations under the process that started them. " +
  "Each second counts its processes, so one active across several seconds counts more than once.";

// TotalsRead is the scope's totals as read, kept with the scope they were read for so another scope's are never shown.
interface TotalsRead {
  key: string;
  totals?: ActivityTotals;
  error?: string;
}

// ClassList is one class's list as read so far, a page at a time (issue #1424): its rows, the cursor of the page after them, whether
// a read is under way or failed, and whether its first page is in.
interface ClassList {
  rows: unknown[];
  cursor?: string;
  loading: boolean;
  failed: boolean;
  loaded: boolean;
}

// Lists is each class's list and the processes their rows name, kept with the scope and search they were read for and the cycle of
// reads that loaded them, and the classes in the order their first pages arrived.
interface Lists {
  key: string;
  cycle: number;
  byClass: ReadonlyMap<TabKey, ClassList>;
  processes: ReadonlyMap<string, ActivityProcess>;
  arrived: readonly TabKey[];
}

// READ_ORDER is the order the classes' first pages are asked for. They are read at once, so it only decides which is shown first when
// several answer together.
const READ_ORDER: readonly TabKey[] = ["network", "dns", "files"];

const EMPTY_LIST: ClassList = { rows: [], loading: false, failed: false, loaded: false };

function emptyLists(key: string, cycle = 0): Lists {
  return { key, cycle, byClass: new Map(), processes: new Map(), arrived: [] };
}

// firstShown is the class shown until the operator picks one: the first whose list arrived with rows, or while none has, the first to
// arrive, so a quick empty list does not hide the one that has something to show.
function firstShown(lists: Lists): TabKey {
  const withRows = lists.arrived.find((cls) => (lists.byClass.get(cls)?.rows.length ?? 0) > 0);
  return withRows ?? (lists.arrived.length > 0 ? lists.arrived[0] : "network");
}

// generationKey names a process generation, which is how a row names the process behind it.
function generationKey(pid: number, pidversion: number): string {
  return `${String(pid)}:${String(pidversion)}`;
}

interface Props {
  // label names the class switch, so two activity views on one page (a session's and a selected process's) are told apart.
  readonly label: string;
  readonly hostId: string;
  readonly scope: ActivityScopeQuery;
  // onProcess, when set, makes each row's process a button that selects it (the session view); otherwise it is plain text. It is
  // handed the process and the moment of the row's first event, which together name the process to read when it is not loaded.
  readonly onProcess?: (process: ActivityProcess, atNs: number) => void;
  // search, when set, lists each class's rows the server finds for it (issue #1452); the totals stay the scope's.
  readonly search?: string;
}

// ActivityTabs shows what a process, its subtree or an agent session touched (issue #1285): its files, network destinations and DNS
// names as totals and lists, each row naming the process behind it. The server matches events to processes by exact generation and
// says how far the scope reached; this says so too.
//
// The totals and each class's list are read at once, and each is shown as it arrives (issue #1550): on a long session the totals are
// the slowest read, and one list was kept waiting on the others.
export function ActivityTabs({ label, hostId, scope, onProcess, search = "" }: Props) {
  const key = `${hostId}/${JSON.stringify(scope)}`;
  // A class's list belongs to the scope and the search it was read for.
  const listKey = `${key}\n${search}`;
  const [read, setRead] = useState<TotalsRead | null>(null);
  // chosen is the class the operator picked. Until they pick one, firstShown picks.
  const [chosen, setChosen] = useState<TabKey | null>(null);
  // dnsByName orders the DNS list by name instead of when each was first seen. It outlives a switch to another tab and back.
  const [dnsByName, setDnsByName] = useState(false);
  const current = read?.key === key ? read : null;
  const [lists, setLists] = useState<Lists>(() => emptyLists(listKey));
  const shown = lists.key === listKey ? lists : emptyLists(listKey);
  // cycle counts the reads of a scope and search. A page lands only in the cycle it was read in, so after a move from A to B and back
  // to A, the first A's late pages are dropped rather than appended to the second's.
  const cycle = useRef(0);

  // loadPage reads a class's page after its cursor, the first page from the empty cursor, and appends it, for the scope and search it
  // was asked for only.
  const loadPage = useCallback((cls: TabKey, cursor: string) => {
    const key = listKey;
    const readIn = cycle.current;
    // A page lands only on the scope it was read for: one read before the operator moved to another scope is dropped, rather than
    // replacing what was loaded for the scope now shown.
    const update = (change: (prior: ClassList) => ClassList, settle: boolean, page?: ActivityPage<unknown>) => {
      setLists((l) => {
        const same = l.key === key && l.cycle === readIn;
        if (settle && !same) return l;
        const base = same ? l : emptyLists(key, readIn);
        const byClass = new Map(base.byClass);
        byClass.set(cls, change(byClass.get(cls) ?? EMPTY_LIST));
        if (!page) return { ...base, byClass };
        const processes = new Map(base.processes);
        for (const p of page.processes ?? []) processes.set(generationKey(p.pid, p.pidversion), p);
        const arrived = base.arrived.includes(cls) ? base.arrived : [...base.arrived, cls];
        return { key, cycle: readIn, byClass, processes, arrived };
      });
    };
    // The cursor being read stays the class's until a page replaces it, so a failed read is offered again.
    update((prior) => ({ ...prior, cursor, loading: true, failed: false }), false);
    getActivityPage<unknown>(hostId, scope, cls, cursor, search)
      .then((page) => {
        update((prior) => ({
          rows: [...prior.rows, ...page.rows], cursor: page.next_cursor, loading: false, failed: false, loaded: true,
        }), true, page);
      })
      .catch(() => { update((prior) => ({ ...prior, loading: false, failed: true }), true); });
  }, [hostId, scope, listKey, search]);

  useEffect(() => {
    cycle.current += 1;
    for (const cls of READ_ORDER) loadPage(cls, "");
  }, [listKey]); // eslint-disable-line react-hooks/exhaustive-deps -- loadPage reads the scope and search listKey names

  useEffect(() => {
    let cancelled = false;
    getActivityTotals(hostId, scope)
      .then((totals) => { if (!cancelled) setRead({ key, totals }); })
      .catch((err: unknown) => { if (!cancelled) setRead({ key, error: err instanceof Error ? err.message : "Unknown error" }); });
    return () => { cancelled = true; };
  }, [key]); // eslint-disable-line react-hooks/exhaustive-deps -- scope is captured; key is its stable identity

  const totals = current?.totals;
  const tab = chosen ?? firstShown(shown);
  const searching = search !== "";
  const processName = (row: ProcessRef): ReactNode => {
    const process = shown.processes.get(generationKey(row.pid, row.pidversion));
    if (!process) return `pid ${String(row.pid)}`;
    const label = `${executableName(process.path)} (${String(row.pid)})`;
    if (!onProcess) return label;
    return (
      <button type="button" className="activity-tabs__process" onClick={() => { onProcess(process, row.first_ns); }}>
        {label}
      </button>
    );
  };
  const note = totals ? scopeNote(totals.scope) : null;
  // A count is left out until the totals are in.
  const tabs: { key: TabKey; label: string; distinct?: number }[] = [
    { key: "files", label: "Files", distinct: totals?.files.distinct },
    { key: "network", label: "Network", distinct: totals?.network.distinct },
    { key: "dns", label: "DNS", distinct: totals?.dns.distinct },
  ];
  const list = shown.byClass.get(tab) ?? EMPTY_LIST;
  const panel = { totals, processName, hasMore: list.cursor !== undefined && list.cursor !== "" };
  const more = <LoadMore cls={tab} list={list} onMore={loadPage} />;

  return (
    <div className="activity-tabs">
      {note && <p className="activity-tabs__note" role="status">{note}</p>}
      {current?.error && <p className="activity-tabs__note" role="status">The totals could not be loaded: {current.error}</p>}
      {/* Toggle buttons rather than an ARIA tablist, which would promise arrow-key navigation this does not implement (see SubNav). */}
      <div role="group" aria-label={label} className="activity-tabs__list">
        {tabs.map((t) => (
          <button
            key={t.key}
            type="button"
            aria-pressed={tab === t.key}
            className="activity-tabs__tab"
            onClick={() => { setChosen(t.key); }}
          >
            {t.label}
            {t.distinct !== undefined && <> <span className="activity-tabs__count">{t.distinct.toLocaleString()}</span></>}
          </button>
        ))}
      </div>
      <div className="activity-tabs__panel">
        {!list.loaded && !list.failed && <EmptyState>{searching ? "Searching..." : "Loading activity..."}</EmptyState>}
        {!list.loaded && list.failed && more}
        {list.loaded && tab === "files" && <FilesPanel {...panel} rows={list.rows as FileActivityRow[]} more={more} />}
        {list.loaded && tab === "network" && <NetworkPanel {...panel} rows={list.rows as NetworkActivityRow[]} more={more} />}
        {list.loaded && tab === "dns" && (
          <DNSPanel {...panel} rows={list.rows as DNSActivityRow[]} more={more} byName={dnsByName} onByName={setDnsByName} />
        )}
      </div>
    </div>
  );
}

// resolversOf is the processes a DNS row names. A row from an older replica during a rolling upgrade has no resolvers: it is one
// process's, so it names that one.
function resolversOf(r: DNSActivityRow): DNSResolver[] {
  return r.resolvers ?? [{ pid: r.pid, pidversion: r.pidversion, first_ns: r.first_ns }];
}

// ProcessRef names the process behind a row, or one of a DNS row's resolvers: its generation, and when it first did what the row
// records, which together name the process to read when it is not loaded.
type ProcessRef = Pick<ActivityRow, "pid" | "pidversion" | "first_ns">;

interface PanelProps<Row> {
  // totals are the scope's, once they are in.
  readonly totals: ActivityTotals | undefined;
  // rows are the class's rows read so far.
  readonly rows: Row[];
  readonly processName: (row: ProcessRef) => ReactNode;
  // hasMore is set while the class has rows not yet read; more offers the next page.
  readonly hasMore: boolean;
  readonly more: ReactNode;
}

// LoadMore offers a class's next page while it has one, and a first page that failed again from the empty cursor it is read from.
function LoadMore({ cls, list, onMore }: {
  readonly cls: TabKey;
  readonly list: ClassList;
  readonly onMore: (cls: TabKey, cursor: string) => void;
}) {
  const { cursor } = list;
  // A first page that failed has shown nothing, so it is offered again rather than as more rows.
  const first = !list.loaded;
  return (
    <>
      {list.failed && (
        <p className="activity-tabs__note" role="status">{first ? "The list could not be loaded." : "The next rows could not be loaded."}</p>
      )}
      {cursor !== undefined && (cursor !== "" || list.failed) && (
        <Button variant="text-link" disabled={list.loading} onClick={() => { onMore(cls, cursor); }}>
          {first ? "Try again" : "Load more rows"}
        </Button>
      )}
    </>
  );
}

// Summary is a class's totals line, once the totals are in, and its cut-list notice while the rows listed are fewer than the class has.
function Summary({ text, more, shown }: { readonly text: string | undefined; readonly more: boolean; readonly shown: number }) {
  return (
    <>
      <p className="activity-tabs__summary">{text ?? "Counting..."}</p>
      {more && (
        <p className="activity-tabs__note" role="status">
          Showing the first {plural(shown, "row", "rows")}; there are more than the list holds.
        </p>
      )}
    </>
  );
}

// Seen is a row's count and when it was first and last seen, each time as an age ("6m ago") with the exact time on hover, as the Agent
// sessions list shows one. A row seen once has no last.
function Seen({ row }: { readonly row: ActivityRow }) {
  const once = row.last_ns === row.first_ns;
  return (
    <>
      <td>{row.count.toLocaleString()}</td>
      <td title={formatNs(row.first_ns)}>{formatRelativeNs(row.first_ns)}</td>
      <td title={once ? undefined : formatNs(row.last_ns)}>{once ? "" : formatRelativeNs(row.last_ns)}</td>
    </>
  );
}

// DaemonName names the daemon behind a socket a connect row reached (issue #1355), when the path is a well-known one. It is a tag,
// which keeps its width while the path beside it shortens, so the cell stays within its cap.
function DaemonName({ path }: { readonly path: string }) {
  const name = socketDaemon(path);
  return name ? <span className="activity-tabs__daemon" title={DAEMON_TITLE}>{name}</span> : null;
}

// HEAD_KEPT_CHARS is how much of a path's start stays in view when even its parent directory and name are too wide for the cell, so the
// root (/Users/..., /Library/...) is never cut away entirely. The cell is monospaced, so a ch is one character.
const HEAD_KEPT_CHARS = 12;

// PATH_KEPT_CHARS is the narrowest a path gets, so that beside a rename's other path it keeps some of its name in view after its head,
// rather than giving its width away in proportion to its length.
const PATH_KEPT_CHARS = 2 * HEAD_KEPT_CHARS;

// TruncatedPath shows a path on one line, giving up width to whatever else its row's cell holds. It is cut in its middle: the start says
// where the file lives (a home directory or a system one) and the parent directory and name say what it is, so both stay in view and
// the ellipsis falls between them. A parent and name too long for the cell on their own are cut at their start, after the path's first
// few characters. The text is the whole path, so a copy keeps it, and it is the tooltip too.
function TruncatedPath({ path }: { readonly path: string }) {
  const { head, tail } = splitPath(path);
  // A head longer than it keeps in view is cut, with its own ellipsis, before the tail is, so that tail's cut needs no second one. A
  // shorter head is never cut, so the tail marks its own.
  const headCut = head.length > HEAD_KEPT_CHARS;
  return (
    <span className="activity-tabs__path" title={path} style={{ minWidth: `${String(Math.min(path.length, PATH_KEPT_CHARS))}ch` }}>
      {head && (
        <span className="activity-tabs__path-head" style={{ minWidth: `${String(Math.min(head.length, HEAD_KEPT_CHARS))}ch` }}>
          {head}
        </span>
      )}
      <span className={headCut ? "activity-tabs__path-tail activity-tabs__path-tail--after-cut" : "activity-tabs__path-tail"}>
        <bdi>{tail}</bdi>
      </span>
    </span>
  );
}

function FilesPanel({ totals, rows, processName, hasMore: listsMore, more }: PanelProps<FileActivityRow>) {
  const files = totals?.files;
  const text = files && `${plural(files.events, "file event", "file events")} on ${plural(files.distinct, "path", "paths")}`;
  return (
    <>
      <Summary text={text} more={listsMore} shown={rows.length} />
      {rows.length > 0 && (
        <Table>
          <thead><tr><th>Operation</th><th>Path</th><th>Process</th><th>Count</th><th>First</th><th>Last</th></tr></thead>
          <tbody>
            {rows.map((r) => (
              <tr key={`${r.operation}|${r.path}|${r.source_path ?? ""}|${r.rolled_up ? "dir" : ""}|${r.processes ? "folded" : ""}|${String(r.pid)}:${String(r.pidversion)}`}>
                <td>{OPERATION_LABELS[r.operation] ?? r.operation}</td>
                <td className="activity-tabs__mono activity-tabs__file-cell">
                  <span className="activity-tabs__file">
                    {r.source_path ? (
                      <>
                        <TruncatedPath path={r.source_path} />{" "}
                        <span className="activity-tabs__arrow">→</span>{" "}
                        <TruncatedPath path={r.path} />
                      </>
                    ) : (
                      <TruncatedPath path={r.path} />
                    )}
                    {r.rolled_up && <span className="activity-tabs__rollup" title={ROLLED_UP_TITLE}>/ and below</span>}
                    {/* A space a copy keeps; the gap alone would run the path into the daemon's name. */}
                    {r.operation === "connect" && <>{" "}<DaemonName path={r.path} /></>}
                  </span>
                </td>
                <td>
                  {r.processes ? (
                    <span title={FOLDED_TITLE}>up to {plural(r.processes, "process", "processes")} under {processName(r)}</span>
                  ) : (
                    processName(r)
                  )}
                </td>
                <Seen row={r} />
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {more}
    </>
  );
}

function NetworkPanel({ totals, rows, processName, hasMore: listsMore, more }: PanelProps<NetworkActivityRow>) {
  const network = totals?.network;
  const text = network &&
    `${plural(network.events, "connection", "connections")} to ${plural(network.distinct, "destination", "destinations")}`;
  return (
    <>
      <Summary text={text} more={listsMore} shown={rows.length} />
      {rows.length > 0 && (
        <Table>
          <thead><tr><th>Destination</th><th>Process</th><th>Count</th><th>First</th><th>Last</th></tr></thead>
          <tbody>
            {rows.map((r) => (
              <tr key={`${r.remote_address}|${String(r.remote_port)}|${r.protocol}|${String(r.pid)}:${String(r.pidversion)}`}>
                <td className="activity-tabs__mono">
                  {hostPort(r.remote_address, r.remote_port)} {r.protocol}
                  {r.remote_hostname && <span className="activity-tabs__hostname"> {r.remote_hostname}</span>}
                </td>
                <td>{processName(r)}</td>
                <Seen row={r} />
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {more}
    </>
  );
}

// DNSPanelProps adds the order of the DNS list: by name when byName is set, otherwise as listed, by when each was first seen.
interface DNSPanelProps extends PanelProps<DNSActivityRow> {
  readonly byName: boolean;
  readonly onByName: (byName: boolean) => void;
}

function DNSPanel({ totals, rows, processName, byName, onByName, hasMore: listsMore, more }: DNSPanelProps) {
  const dns = totals?.dns;
  const text = dns && `${plural(dns.events, "query", "queries")} for ${plural(dns.distinct, "name", "names")}`;
  // The sort orders the rows listed. A cut list's later rows join the order as they are read.
  const shown = useMemo(
    () => (byName ? [...rows].sort((a, b) => a.query_name.localeCompare(b.query_name) || a.first_ns - b.first_ns) : rows),
    [rows, byName],
  );
  return (
    <>
      <Summary text={text} more={listsMore} shown={rows.length} />
      {rows.length > 0 && (
        <Table>
          <thead>
            <tr>
              {/* A real button, so the sort is reachable by keyboard; the th's aria-sort states the order. */}
              <th aria-sort={byName ? "ascending" : "none"}>
                <button
                  type="button"
                  className="activity-tabs__sort"
                  aria-pressed={byName}
                  title={byName ? "Listed by name. Select to list by when each was first seen." : "Select to list by name."}
                  onClick={() => { onByName(!byName); }}
                >
                  Name{byName ? " ▲" : ""}
                </button>
              </th>
              <th>Answers</th><th>Process</th><th>Count</th><th>First</th><th>Last</th>
            </tr>
          </thead>
          <tbody>
            {shown.map((r) => {
              const resolvers = resolversOf(r);
              const count = r.process_count ?? resolvers.length;
              return (
              <tr key={`${r.query_name}:${String(resolvers[0].pid)}:${String(resolvers[0].pidversion)}`}>
                <td className="activity-tabs__mono">{r.query_name}</td>
                {/* An answer list wraps within its cap, so a name answered by many addresses grows down rather than sideways. */}
                <td className="activity-tabs__mono">
                  <span className="activity-tabs__answers">{r.addresses.join(", ")}</span>
                </td>
                {/* Every process that resolved the name, wrapped within the cap as the answers are. */}
                <td>
                  <span className="activity-tabs__resolvers">
                    {resolvers.map((p, i) => (
                      <Fragment key={`${String(p.pid)}:${String(p.pidversion)}`}>
                        {i > 0 && ", "}
                        {processName(p)}
                      </Fragment>
                    ))}
                    {count > resolvers.length && `, and ${plural(count - resolvers.length, "other", "others")}`}
                  </span>
                </td>
                <Seen row={r} />
              </tr>
              );
            })}
          </tbody>
        </Table>
      )}
      {more}
    </>
  );
}
