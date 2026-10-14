import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router";
import "./SessionView.scss";
import { getSessionTree, listAgentSessions, listAlerts } from "../../api";
import type { AgentSessionSummary, Alert, TreeResponse } from "../../types";
import { PageHeader } from "../ui/PageHeader";
import { Toggle } from "../ui/Toggle";
import { Badge } from "../ui/Badge";
import { EmptyState } from "../ui/Table";
import { severityBadgeVariant } from "../ui/severity";
import { formatNs, basename } from "../Search/format";
import { useHostNames } from "../useHostNames";
import { NANOSECONDS_PER_MILLISECOND } from "../../constants";
import { buildToolCalls, summarizeSession, type SessionRow, type ToolCall } from "./sessionTree";
import { commandLine } from "./toolCommand";

// AUTO_EXPAND_CALLS is the most tool calls a session can have and still open with every call expanded. A longer session opens with
// only the calls an alert was raised in expanded, so the operator starts from what was flagged.
const AUTO_EXPAND_CALLS = 10;
// MAX_SESSION_ALERTS bounds the session's alert read.
const MAX_SESSION_ALERTS = 1000;
// SEVERITY_ORDER ranks severities from least to most severe; an unknown severity ranks below them all.
const SEVERITY_ORDER: readonly string[] = ["low", "medium", "high", "critical"];

// SessionRead is everything the view reads for one session, kept with the session it was for so another session's results are never
// shown. The tree is required; the record and the alerts decorate it, and their failures are reported without hiding the tree.
interface SessionRead {
  key: string;
  tree?: TreeResponse;
  error?: string;
  record?: AgentSessionSummary;
  alerts?: Alert[];
  alertsFailed?: boolean;
}

function message(err: unknown): string {
  return err instanceof Error ? err.message : "Unknown error";
}

async function readSession(key: string, hostId: string, sessionId: string): Promise<SessionRead> {
  const [tree, record, alerts] = await Promise.all([
    getSessionTree(hostId, sessionId).catch((err: unknown) => message(err)),
    listAgentSessions({ host_id: hostId, session: sessionId })
      .then((res) => res.rows[0] as AgentSessionSummary | undefined)
      .catch(() => undefined),
    listAlerts({ host_id: hostId, agent_session: sessionId, limit: MAX_SESSION_ALERTS }).catch(() => null),
  ]);
  if (typeof tree === "string") return { key, error: tree };
  return { key, tree, record, alerts: alerts ?? [], alertsFailed: alerts === null };
}

// SessionView answers "what did this agent session do" (issue #1284): the session's processes in the order its tool calls ran, one
// group per call labelled with the command the agent ran, and under it everything that call started, daemons included. The shell
// plumbing that runs a call is hidden unless the operator turns on verbose. Alerts and privilege changes are badged where they
// happened, and a call an alert was raised in opens expanded.
export function SessionView() {
  const { hostId = "", sessionId = "" } = useParams();
  const hostNames = useHostNames();
  const [verbose, setVerbose] = useState(false);
  const readKey = `${hostId}/${sessionId}`;
  const [read, setRead] = useState<SessionRead | null>(null);
  const current = read?.key === readKey ? read : null;
  const tree = current?.tree ?? null;

  useEffect(() => {
    let cancelled = false;
    void readSession(readKey, hostId, sessionId).then((res) => { if (!cancelled) setRead(res); });
    return () => { cancelled = true; };
  }, [hostId, sessionId, readKey]);

  const alertsByProcess = useMemo(() => {
    const byProcess = new Map<number, Alert[]>();
    for (const alert of current?.alerts ?? []) byProcess.set(alert.process_id, [...(byProcess.get(alert.process_id) ?? []), alert]);
    return byProcess;
  }, [current?.alerts]);
  const calls = useMemo(
    () => (tree ? buildToolCalls(tree.roots, { verbose, keep: new Set(alertsByProcess.keys()) }) : []),
    [tree, verbose, alertsByProcess],
  );
  const summary = useMemo(() => summarizeSession(tree?.roots ?? []), [tree]);

  const record = current?.record;
  const agentName = record?.agent_name ?? summary.agentName;
  const agentVersion = record?.agent_version ?? summary.agentVersion;
  const title = agentName ? `${agentName}${agentVersion ? ` ${agentVersion}` : ""}` : "Agent session";
  const firstNs = record?.first_seen_ns ?? summary.firstNs;
  const lastNs = Math.max(record?.last_seen_ns ?? 0, summary.lastNs ?? 0) || undefined;
  const project = record?.cwd ?? summary.project;
  const subtitle = (
    <span className="session-view__facts">
      {project && <span className="session-view__mono">{project}</span>}
      <span>{hostNames.get(hostId) ?? hostId}</span>
      {firstNs !== undefined && lastNs !== undefined && <span>{`${formatNs(firstNs)} to ${formatNs(lastNs)}`}</span>}
      {tree && <span>{summary.processCount.toLocaleString()} processes</span>}
      {record?.agent_pid !== undefined && <span>agent pid {record.agent_pid}</span>}
      <span className="session-view__mono" title={sessionId}>Session {sessionId}</span>
    </span>
  );

  return (
    <div className="session-view">
      <Link to="/sessions" className="session-view__back">All sessions</Link>
      <PageHeader
        title={title}
        subtitle={subtitle}
        actions={(
          <Toggle id="session-verbose" label="Show shell plumbing" checked={verbose} onChange={(e) => { setVerbose(e.target.checked); }} />
        )}
      />
      <SessionNotices read={current} />
      <SessionBody read={current} calls={calls} hostId={hostId} alertsByProcess={alertsByProcess} />
    </div>
  );
}

// SessionNotices says what the view is not showing: processes past the read's limit, processes retention removed, and alerts that
// could not be read.
function SessionNotices({ read }: { readonly read: SessionRead | null }) {
  const tree = read?.tree;
  if (!tree) return null;
  const retainedFrom = tree.retained_from_ns;
  const beganBeforeRetention = retainedFrom !== undefined && read.record !== undefined && read.record.first_seen_ns < retainedFrom;
  return (
    <>
      {tree.truncated && (
        <p className="session-view__notice" role="status">
          Showing the first {tree.returned.toLocaleString()} of {tree.total_matched_capped ? "more than " : ""}
          {tree.total_matched.toLocaleString()} processes in this session.
        </p>
      )}
      {beganBeforeRetention && (
        <p className="session-view__notice" role="status">
          This session began before {formatNs(retainedFrom)}, and processes that finished before then have been removed by retention.
        </p>
      )}
      {read.alertsFailed && (
        <p className="session-view__notice" role="status">This session&apos;s alerts could not be loaded, so none are shown.</p>
      )}
    </>
  );
}

function SessionBody({ read, calls, hostId, alertsByProcess }: {
  readonly read: SessionRead | null;
  readonly calls: ToolCall[];
  readonly hostId: string;
  readonly alertsByProcess: ReadonlyMap<number, Alert[]>;
}) {
  if (read?.error) return <EmptyState>Error: {read.error}</EmptyState>;
  if (!read?.tree) return <EmptyState>Loading...</EmptyState>;
  if (calls.length === 0) return <EmptyState>No processes are recorded for this session.</EmptyState>;
  const expandAll = calls.length <= AUTO_EXPAND_CALLS;
  return (
    <ol className="session-view__calls">
      {calls.map((call) => {
        const alerts = call.processIds.flatMap((id) => alertsByProcess.get(id) ?? []);
        return (
          <ToolCallGroup
            key={call.key}
            call={call}
            hostId={hostId}
            alerts={alerts}
            alertsByProcess={alertsByProcess}
            defaultOpen={expandAll || alerts.length > 0}
          />
        );
      })}
    </ol>
  );
}

// highestSeverity is the most severe of the alerts' severities, for the group's badge.
function highestSeverity(alerts: readonly Alert[]): string {
  return alerts.reduce((top, a) => (SEVERITY_ORDER.indexOf(a.severity) > SEVERITY_ORDER.indexOf(top) ? a.severity : top), "");
}

function ToolCallGroup({ call, hostId, alerts, alertsByProcess, defaultOpen }: {
  readonly call: ToolCall;
  readonly hostId: string;
  readonly alerts: readonly Alert[];
  readonly alertsByProcess: ReadonlyMap<number, Alert[]>;
  readonly defaultOpen: boolean;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <li className="session-view__call">
      <button
        type="button"
        className="session-view__call-header"
        aria-expanded={open}
        onClick={() => { setOpen(!open); }}
      >
        <code className="session-view__command">{call.command}</code>
        <span className="session-view__meta">
          {formatNs(call.startNs)} · {call.processCount.toLocaleString()} {call.processCount === 1 ? "process" : "processes"}
        </span>
        {alerts.length > 0 && (
          <Badge variant={severityBadgeVariant(highestSeverity(alerts))}>
            {alerts.length} {alerts.length === 1 ? "alert" : "alerts"}
          </Badge>
        )}
      </button>
      {open && call.rows.length > 0 && <ProcessRows rows={call.rows} hostId={hostId} alertsByProcess={alertsByProcess} />}
    </li>
  );
}

function ProcessRows({ rows, hostId, alertsByProcess }: {
  readonly rows: SessionRow[];
  readonly hostId: string;
  readonly alertsByProcess: ReadonlyMap<number, Alert[]>;
}) {
  return (
    <ul className="session-view__rows">
      {rows.map(({ node, children, uidChange }) => (
        <li key={node.id}>
          <div className="session-view__row">
            <Link to={hostHref(hostId, node.id, node.fork_time_ns)} className="session-view__name">{basename(node.path)}</Link>
            <span className="session-view__mono session-view__cmdline">{commandLine(node.path, node.args)}</span>
            <span className="session-view__meta">
              pid {node.pid} · {formatNs(node.fork_time_ns)}
              {node.exit_code !== undefined && node.exit_reason === "event" ? ` · exit ${String(node.exit_code)}` : ""}
            </span>
            {uidChange && (
              <span title={`Runs as uid ${String(uidChange.to)}; its creator ran as uid ${String(uidChange.from)}`}>
                <Badge variant="high">{uidChange.to === 0 ? "runs as root" : `runs as uid ${String(uidChange.to)}`}</Badge>
              </span>
            )}
            {(alertsByProcess.get(node.id) ?? []).map((alert) => (
              <Link key={alert.id} to={`/alerts/${String(alert.id)}`} className="session-view__alert">
                <Badge variant={severityBadgeVariant(alert.severity)}>{alert.title}</Badge>
              </Link>
            ))}
          </div>
          {children.length > 0 && <ProcessRows rows={children} hostId={hostId} alertsByProcess={alertsByProcess} />}
        </li>
      ))}
    </ul>
  );
}

// hostHref opens a process in its host's graph, selected and anchored at its fork time: the graph's ?process= takes the row id.
function hostHref(hostId: string, processId: number, forkNs: number): string {
  const atMs = Math.floor(forkNs / NANOSECONDS_PER_MILLISECOND) + 1;
  return `/hosts/${encodeURIComponent(hostId)}?process=${String(processId)}&at=${String(atMs)}`;
}
