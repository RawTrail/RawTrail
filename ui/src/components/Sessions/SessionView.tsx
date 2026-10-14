import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router";
import "./SessionView.scss";
import { getSessionTree } from "../../api";
import type { TreeResponse } from "../../types";
import { PageHeader } from "../ui/PageHeader";
import { Toggle } from "../ui/Toggle";
import { EmptyState } from "../ui/Table";
import { formatNs, basename } from "../Search/format";
import { useHostNames } from "../useHostNames";
import { NANOSECONDS_PER_MILLISECOND } from "../../constants";
import { buildToolCalls, summarizeSession, type SessionRow, type ToolCall } from "./sessionTree";
import { commandLine } from "./toolCommand";

// SessionView answers "what did this agent session do" (issue #1284): the session's processes in the order its tool calls ran, one
// group per call labelled with the command the agent ran, and under it everything that call started, daemons included. The shell
// plumbing that runs a call is hidden unless the operator turns on verbose.
export function SessionView() {
  const { hostId = "", sessionId = "" } = useParams();
  const hostNames = useHostNames();
  const [verbose, setVerbose] = useState(false);
  // The read's outcome is kept with the session it was for, so navigating to another session shows neither the previous session's
  // processes nor its error while the new read is in flight.
  const readKey = `${hostId}/${sessionId}`;
  const [read, setRead] = useState<{ key: string; tree?: TreeResponse; error?: string } | null>(null);
  const tree = read?.key === readKey ? (read.tree ?? null) : null;
  const error = read?.key === readKey ? (read.error ?? null) : null;

  useEffect(() => {
    let cancelled = false;
    getSessionTree(hostId, sessionId)
      .then((res) => { if (!cancelled) setRead({ key: readKey, tree: res }); })
      .catch((err: unknown) => {
        if (!cancelled) setRead({ key: readKey, error: err instanceof Error ? err.message : "Unknown error" });
      });
    return () => { cancelled = true; };
  }, [hostId, sessionId, readKey]);

  const calls = useMemo(() => (tree ? buildToolCalls(tree.roots, verbose) : []), [tree, verbose]);
  const summary = useMemo(() => summarizeSession(tree?.roots ?? []), [tree]);

  const title = summary.agentName ? `${summary.agentName}${summary.agentVersion ? ` ${summary.agentVersion}` : ""}` : "Agent session";
  const span = summary.firstNs === undefined || summary.lastNs === undefined
    ? undefined
    : `${formatNs(summary.firstNs)} to ${formatNs(summary.lastNs)}`;
  const subtitle = (
    <span className="session-view__facts">
      {summary.project && <span className="session-view__mono">{summary.project}</span>}
      <span>{hostNames.get(hostId) ?? hostId}</span>
      {span && <span>{span}</span>}
      {tree && <span>{summary.processCount.toLocaleString()} processes</span>}
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
      {tree?.truncated && (
        <p className="session-view__notice" role="status">
          Showing the first {tree.returned.toLocaleString()} of {tree.total_matched_capped ? "more than " : ""}
          {tree.total_matched.toLocaleString()} processes in this session.
        </p>
      )}
      <SessionBody tree={tree} error={error} calls={calls} hostId={hostId} />
    </div>
  );
}

function SessionBody({ tree, error, calls, hostId }: {
  readonly tree: TreeResponse | null;
  readonly error: string | null;
  readonly calls: ToolCall[];
  readonly hostId: string;
}) {
  if (error) return <EmptyState>Error: {error}</EmptyState>;
  if (!tree) return <EmptyState>Loading...</EmptyState>;
  if (calls.length === 0) return <EmptyState>No processes are recorded for this session.</EmptyState>;
  return (
    <ol className="session-view__calls">
      {calls.map((call) => <ToolCallGroup key={call.key} call={call} hostId={hostId} />)}
    </ol>
  );
}

function ToolCallGroup({ call, hostId }: { readonly call: ToolCall; readonly hostId: string }) {
  const [open, setOpen] = useState(true);
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
      </button>
      {open && call.rows.length > 0 && <ProcessRows rows={call.rows} hostId={hostId} />}
    </li>
  );
}

function ProcessRows({ rows, hostId }: { readonly rows: SessionRow[]; readonly hostId: string }) {
  return (
    <ul className="session-view__rows">
      {rows.map(({ node, children }) => (
        <li key={node.id}>
          <div className="session-view__row">
            <Link to={hostHref(hostId, node.id, node.fork_time_ns)} className="session-view__name">{basename(node.path)}</Link>
            <span className="session-view__mono session-view__cmdline">{commandLine(node.path, node.args)}</span>
            <span className="session-view__meta">
              pid {node.pid} · {formatNs(node.fork_time_ns)}
              {node.exit_code !== undefined && node.exit_reason === "event" ? ` · exit ${String(node.exit_code)}` : ""}
            </span>
          </div>
          {children.length > 0 && <ProcessRows rows={children} hostId={hostId} />}
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
