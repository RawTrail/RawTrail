import { useEffect, useState, type ReactNode } from "react";
import "./ActivityTabs.scss";
import { getActivity, type ActivityScopeQuery } from "../../api";
import type { ActivityResponse, ActivityRow, ActivityScope } from "../../types";
import { Table, EmptyState } from "../ui/Table";
import { formatNs, executableName, hostPort, plural } from "../Search/format";

type TabKey = "files" | "network" | "dns";

// OPERATION_LABELS names a watched-path event's type as an operation. An AI agent's file activity (issue #1287) already carries its
// operation (create, modify, rename, delete), which is shown as it is.
const OPERATION_LABELS: Record<string, string> = {
  open: "open",
  file_rename: "rename",
  file_truncate: "truncate",
  file_delete: "delete",
};

// ROLLED_UP_TITLE explains a folded row: the extension sends at most a set number of rows a second, so a busy window's files are
// counted under their directory.
const ROLLED_UP_TITLE = "Too many files changed at once to list each: this row counts the files under this directory.";

interface ActivityRead {
  key: string;
  activity?: ActivityResponse;
  error?: string;
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
        {tab === "files" && <FilesPanel activity={activity} processName={processName} />}
        {tab === "network" && <NetworkPanel activity={activity} processName={processName} />}
        {tab === "dns" && <DNSPanel activity={activity} processName={processName} />}
      </div>
    </div>
  );
}

interface PanelProps {
  readonly activity: ActivityResponse;
  readonly processName: (row: ActivityRow) => ReactNode;
}

// Summary is a class's totals line, and its cut-list notice when the server listed fewer rows than the class has.
function Summary({ text, truncated, shown }: { readonly text: string; readonly truncated: boolean; readonly shown: number }) {
  return (
    <>
      <p className="activity-tabs__summary">{text}</p>
      {truncated && (
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

function FilesPanel({ activity, processName }: PanelProps) {
  const { files } = activity;
  const text = `${plural(files.events, "file event", "file events")} on ${plural(files.distinct, "path", "paths")}`;
  return (
    <>
      <Summary text={text} truncated={files.truncated} shown={files.rows.length} />
      {files.rows.length > 0 && (
        <Table>
          <thead><tr><th>Operation</th><th>Path</th><th>Process</th><th>Count</th><th>First</th><th>Last</th></tr></thead>
          <tbody>
            {files.rows.map((r) => (
              <tr key={`${r.operation}|${r.path}|${r.source_path ?? ""}|${r.rolled_up ? "dir" : ""}|${String(r.pid)}:${String(r.pidversion)}`}>
                <td>{OPERATION_LABELS[r.operation] ?? r.operation}</td>
                <td className="activity-tabs__mono">
                  {r.source_path ? `${r.source_path} → ${r.path}` : r.path}
                  {r.rolled_up && <span className="activity-tabs__rollup" title={ROLLED_UP_TITLE}>/ and below</span>}
                </td>
                <td>{processName(r)}</td>
                <Seen row={r} />
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </>
  );
}

function NetworkPanel({ activity, processName }: PanelProps) {
  const { network } = activity;
  const text = `${plural(network.events, "connection", "connections")} to ${plural(network.distinct, "destination", "destinations")}`;
  return (
    <>
      <Summary text={text} truncated={network.truncated} shown={network.rows.length} />
      {network.rows.length > 0 && (
        <Table>
          <thead><tr><th>Destination</th><th>Process</th><th>Count</th><th>First</th><th>Last</th></tr></thead>
          <tbody>
            {network.rows.map((r) => (
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
    </>
  );
}

function DNSPanel({ activity, processName }: PanelProps) {
  const { dns } = activity;
  const text = `${plural(dns.events, "query", "queries")} for ${plural(dns.distinct, "name", "names")}`;
  return (
    <>
      <Summary text={text} truncated={dns.truncated} shown={dns.rows.length} />
      {dns.rows.length > 0 && (
        <Table>
          <thead><tr><th>Name</th><th>Answers</th><th>Process</th><th>Count</th><th>First</th><th>Last</th></tr></thead>
          <tbody>
            {dns.rows.map((r) => (
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
    </>
  );
}
