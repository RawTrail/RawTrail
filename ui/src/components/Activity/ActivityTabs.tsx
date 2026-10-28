import { useCallback, useEffect, useState, type ReactNode } from "react";
import "./ActivityTabs.scss";
import { getActivity, getActivityPage, type ActivityScopeQuery } from "../../api";
import type {
  ActivityResponse, ActivityRow, ActivityScope, DNSActivityRow, FileActivityRow, NetworkActivityRow,
} from "../../types";
import { Button } from "../ui/Button";
import { Table, EmptyState } from "../ui/Table";
import { formatNs, executableName, hostPort, plural } from "../Search/format";
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

interface ActivityRead {
  key: string;
  activity?: ActivityResponse;
  error?: string;
}

// ClassMore is the rows of one class read past the rollup's cut list (issue #1424), the cursor of the page after them, and whether
// that read is under way or failed.
interface ClassMore {
  rows: unknown[];
  cursor?: string;
  loading: boolean;
  failed: boolean;
}

// More is each class's rows read past its list, kept with the scope they were read for so another scope's are never shown.
interface More {
  key: string;
  byClass: ReadonlyMap<TabKey, ClassMore>;
}

interface Props {
  // label names the class switch, so two activity views on one page (a session's and a selected process's) are told apart.
  readonly label: string;
  readonly hostId: string;
  readonly scope: ActivityScopeQuery;
  // onProcess, when set, makes each row's process a button that selects it (the session view); otherwise it is plain text.
  readonly onProcess?: (processId: number) => void;
}

// scopeNote says how much of the scope the activity reflects, or null when it reflects all of it.
function scopeNote(scope: ActivityScope): string | null {
  // A truncated scope comes first: when the processes read carry no generation, the unread ones still might, so all that can be said
  // is that the activity is partial.
  if (scope.truncated) return "This scope has more processes than are read, so its activity is partial.";
  if (!scope.applied) {
    return scope.processes === 0
      ? "No stored process matches this scope, so there is no activity to show."
      : "None of this scope's processes carry a process generation, so their activity cannot be read.";
  }
  if (scope.unmatched > 0) {
    return `${plural(scope.unmatched, "process carries", "processes carry")} no process generation, so their activity is not counted.`;
  }
  return null;
}

// ActivityTabs shows what a process, its subtree or an agent session touched (issue #1285): its files, network destinations and DNS
// names as totals and lists, each row naming the process behind it. The server matches events to processes by exact generation and
// says how far the scope reached; this says so too.
export function ActivityTabs({ label, hostId, scope, onProcess }: Props) {
  const key = `${hostId}/${JSON.stringify(scope)}`;
  const [read, setRead] = useState<ActivityRead | null>(null);
  const [tab, setTab] = useState<TabKey>("network");
  const current = read?.key === key ? read : null;
  const [more, setMore] = useState<More>({ key, byClass: new Map() });
  const extra: ReadonlyMap<TabKey, ClassMore> = more.key === key ? more.byClass : new Map();

  // loadMore reads a class's next page after its cursor and appends it, for the scope it was asked for only.
  const loadMore = useCallback((cls: TabKey, cursor: string) => {
    const update = (change: (prior: ClassMore) => ClassMore) => {
      setMore((m) => {
        const byClass = new Map(m.key === key ? m.byClass : []);
        byClass.set(cls, change(byClass.get(cls) ?? { rows: [], loading: false, failed: false }));
        return { key, byClass };
      });
    };
    // The cursor being read stays the class's until a page replaces it, so a failed read is offered again.
    update((prior) => ({ ...prior, cursor, loading: true, failed: false }));
    getActivityPage<unknown>(hostId, scope, cls, cursor)
      .then((page) => {
        update((prior) => ({ rows: [...prior.rows, ...page.rows], cursor: page.next_cursor, loading: false, failed: false }));
      })
      .catch(() => { update((prior) => ({ ...prior, loading: false, failed: true })); });
  }, [hostId, scope, key]);

  useEffect(() => {
    let cancelled = false;
    getActivity(hostId, scope)
      .then((activity) => { if (!cancelled) setRead({ key, activity }); })
      .catch((err: unknown) => { if (!cancelled) setRead({ key, error: err instanceof Error ? err.message : "Unknown error" }); });
    return () => { cancelled = true; };
  }, [key]); // eslint-disable-line react-hooks/exhaustive-deps -- scope is captured; key is its stable identity

  if (current?.error) return <EmptyState>Error: {current.error}</EmptyState>;
  const activity = current?.activity;
  if (!activity) return <EmptyState>Loading activity...</EmptyState>;

  const processName = (row: ActivityRow): ReactNode => {
    const process = activity.processes.find((p) => p.pid === row.pid && p.pidversion === row.pidversion);
    if (!process) return `pid ${String(row.pid)}`;
    const label = `${executableName(process.path)} (${String(row.pid)})`;
    if (!onProcess) return label;
    return (
      <button type="button" className="activity-tabs__process" onClick={() => { onProcess(process.id); }}>
        {label}
      </button>
    );
  };
  const note = scopeNote(activity.scope);
  const tabs: { key: TabKey; label: string; distinct: number }[] = [
    { key: "files", label: "Files", distinct: activity.files.distinct },
    { key: "network", label: "Network", distinct: activity.network.distinct },
    { key: "dns", label: "DNS", distinct: activity.dns.distinct },
  ];

  return (
    <div className="activity-tabs">
      {note && <p className="activity-tabs__note" role="status">{note}</p>}
      {/* Toggle buttons rather than an ARIA tablist, which would promise arrow-key navigation this does not implement (see SubNav). */}
      <div role="group" aria-label={label} className="activity-tabs__list">
        {tabs.map((t) => (
          <button
            key={t.key}
            type="button"
            aria-pressed={tab === t.key}
            className="activity-tabs__tab"
            onClick={() => { setTab(t.key); }}
          >
            {t.label} <span className="activity-tabs__count">{t.distinct.toLocaleString()}</span>
          </button>
        ))}
      </div>
      <div className="activity-tabs__panel">
        {tab === "files" && (
          <FilesPanel
            activity={activity}
            rows={[...activity.files.rows, ...(extra.get("files")?.rows ?? []) as FileActivityRow[]]}
            processName={processName}
            hasMore={hasMore(activity.files, extra.get("files"))}
            more={<LoadMore cls="files" first={activity.files.next_cursor} extra={extra.get("files")} onMore={loadMore} />}
          />
        )}
        {tab === "network" && (
          <NetworkPanel
            activity={activity}
            rows={[...activity.network.rows, ...(extra.get("network")?.rows ?? []) as NetworkActivityRow[]]}
            processName={processName}
            hasMore={hasMore(activity.network, extra.get("network"))}
            more={<LoadMore cls="network" first={activity.network.next_cursor} extra={extra.get("network")} onMore={loadMore} />}
          />
        )}
        {tab === "dns" && (
          <DNSPanel
            activity={activity}
            rows={[...activity.dns.rows, ...(extra.get("dns")?.rows ?? []) as DNSActivityRow[]]}
            processName={processName}
            hasMore={hasMore(activity.dns, extra.get("dns"))}
            more={<LoadMore cls="dns" first={activity.dns.next_cursor} extra={extra.get("dns")} onMore={loadMore} />}
          />
        )}
      </div>
    </div>
  );
}

interface PanelProps<Row> {
  readonly activity: ActivityResponse;
  // rows are the class's rows the rollup listed and those read past it.
  readonly rows: Row[];
  readonly processName: (row: ActivityRow) => ReactNode;
  // hasMore is set while the class has rows not yet listed; more offers the next page.
  readonly hasMore: boolean;
  readonly more: ReactNode;
}

// hasMore reports a class with rows not yet listed: one whose last page read names a next page, or whose cut list does.
function hasMore(listed: { truncated: boolean }, extra: ClassMore | undefined): boolean {
  return extra ? extra.cursor !== undefined : listed.truncated;
}

// LoadMore offers a class's next page while it has one: after the rollup's cut list, from the cursor it named, and after a page
// read since, from that page's.
function LoadMore({ cls, first, extra, onMore }: {
  readonly cls: TabKey;
  readonly first: string | undefined;
  readonly extra: ClassMore | undefined;
  readonly onMore: (cls: TabKey, cursor: string) => void;
}) {
  const cursor = extra ? extra.cursor : first;
  return (
    <>
      {extra?.failed && <p className="activity-tabs__note" role="status">The next rows could not be loaded.</p>}
      {cursor && (
        <Button variant="text-link" disabled={extra?.loading === true} onClick={() => { onMore(cls, cursor); }}>
          Load more rows
        </Button>
      )}
    </>
  );
}

// Summary is a class's totals line, and its cut-list notice while the rows listed are fewer than the class has.
function Summary({ text, more, shown }: { readonly text: string; readonly more: boolean; readonly shown: number }) {
  return (
    <>
      <p className="activity-tabs__summary">{text}</p>
      {more && (
        <p className="activity-tabs__note" role="status">
          Showing the first {plural(shown, "row", "rows")}; there are more than the list holds.
        </p>
      )}
    </>
  );
}

function Seen({ row }: { readonly row: ActivityRow }) {
  return (
    <>
      <td>{row.count.toLocaleString()}</td>
      <td>{formatNs(row.first_ns)}</td>
      <td>{row.last_ns === row.first_ns ? "" : formatNs(row.last_ns)}</td>
    </>
  );
}

// DaemonName names the daemon behind a socket a connect row reached (issue #1355), when the path is a well-known one. It is a tag,
// which keeps its width while the path beside it shortens, so the cell stays within its cap.
function DaemonName({ path }: { readonly path: string }) {
  const name = socketDaemon(path);
  return name ? <span className="activity-tabs__daemon" title={DAEMON_TITLE}>{name}</span> : null;
}

// TruncatedPath shows a path on one line, giving up width to whatever else its row's capped cell holds. It is cut at its start, so the
// file name, the part an operator reads for, stays in view, and the whole path is its tooltip. A long path otherwise stretched the
// table past the panel, with nothing to say it scrolled.
function TruncatedPath({ path }: { readonly path: string }) {
  return (
    <span className="activity-tabs__path" title={path}>
      <bdi>{path}</bdi>
    </span>
  );
}

function FilesPanel({ activity, rows, processName, hasMore: listsMore, more }: PanelProps<FileActivityRow>) {
  const { files } = activity;
  const text = `${plural(files.events, "file event", "file events")} on ${plural(files.distinct, "path", "paths")}`;
  return (
    <>
      <Summary text={text} more={listsMore} shown={rows.length} />
      {rows.length > 0 && (
        <Table>
          <thead><tr><th>Operation</th><th>Path</th><th>Process</th><th>Count</th><th>First</th><th>Last</th></tr></thead>
          <tbody>
            {rows.map((r) => (
              <tr key={`${r.operation}|${r.path}|${r.source_path ?? ""}|${r.rolled_up ? "dir" : ""}|${String(r.pid)}:${String(r.pidversion)}`}>
                <td>{OPERATION_LABELS[r.operation] ?? r.operation}</td>
                <td className="activity-tabs__mono">
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

function NetworkPanel({ activity, rows, processName, hasMore: listsMore, more }: PanelProps<NetworkActivityRow>) {
  const { network } = activity;
  const text = `${plural(network.events, "connection", "connections")} to ${plural(network.distinct, "destination", "destinations")}`;
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

function DNSPanel({ activity, rows, processName, hasMore: listsMore, more }: PanelProps<DNSActivityRow>) {
  const { dns } = activity;
  const text = `${plural(dns.events, "query", "queries")} for ${plural(dns.distinct, "name", "names")}`;
  return (
    <>
      <Summary text={text} more={listsMore} shown={rows.length} />
      {rows.length > 0 && (
        <Table>
          <thead><tr><th>Name</th><th>Answers</th><th>Process</th><th>Count</th><th>First</th><th>Last</th></tr></thead>
          <tbody>
            {rows.map((r) => (
              <tr key={`${r.query_name}|${String(r.pid)}:${String(r.pidversion)}`}>
                <td className="activity-tabs__mono">{r.query_name}</td>
                <td className="activity-tabs__mono">{r.addresses.join(", ")}</td>
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
