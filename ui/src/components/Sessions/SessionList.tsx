import { useCallback } from "react";
import { Link } from "react-router";
import "../Search/SearchPage.scss";
import { listAgentSessions } from "../../api";
import type { AgentSessionSummary } from "../../types";
import { Badge } from "../ui/Badge";
import { Table } from "../ui/Table";
import { useCursorList, type CursorPage } from "../Search/useCursorList";
import { SearchResultsFrame } from "../Search/SearchResultsFrame";
import { formatNs } from "../Search/format";
import { useHostNames } from "../useHostNames";
import { formatElapsedNs, formatRelativeNs } from "../../time";
import { sessionPath } from "./sessionPath";
import { PromptSearch } from "./PromptSearch";
import { PermissionAction, useCan } from "../../permissions-core";
import { formatTokens } from "./sessionUsage";
import { useProfile } from "../../profile-core";

// SESSION_ID_PREFIX is how much of a session id the table shows; the full id is the cell's title.
const SESSION_ID_PREFIX = 8;

// LIST_KEY names the list's one result set for useCursorList, which reloads when its key changes.
const LIST_KEY = "all";

// BYPASS_TITLE explains a row's bypass badge, which a fork of a bypassed session carries without having bypassed itself.
const BYPASS_TITLE = "Ran in bypass mode, or was forked from a session that did";

// SessionList is the AI agent sessions page (issue #1283): every session the fleet's agents ran, most recently active first, with the
// project each worked in and how much it did. The server pages the list.
export function SessionList() {
  const hostNames = useHostNames();
  // Searching prompts reads what people typed, so it is offered only to an operator who may read prompts (issue #1428).
  const can = useCan();
  const canReadPrompts = can(PermissionAction.AgentPromptRead);
  const { profile } = useProfile();

  const fetchPage = useCallback(
    async (cursor: string): Promise<CursorPage<AgentSessionSummary>> => {
      const res = await listAgentSessions({}, cursor || undefined);
      return { rows: res.rows, nextCursor: res.next_cursor, total: res.total_matched };
    },
    [],
  );
  const { rows, total, loading, error, hasMore, loadMore } = useCursorList(LIST_KEY, fetchPage);
  // A laptop with no session at all is most likely still being set up, so it points at the checklist (issue #1436).
  const unsetLaptop = profile === "laptop" && can(PermissionAction.HostRead) && !loading && !error && total === 0;

  return (
    <div className="search-page">
      {canReadPrompts && <PromptSearch />}
      {unsetLaptop && (
        <p>
          No sessions yet. <Link to="/setup">Set up this Mac</Link> to approve what it needs, then run claude in a terminal.
        </p>
      )}
      <SearchResultsFrame
        loading={loading}
        error={error}
        count={rows.length}
        total={total}
        hasMore={hasMore}
        onLoadMore={loadMore}
        emptyLabel="No AI agent sessions match."
      >
        <Table>
          <thead>
            <tr>
              <th>Agent</th>
              <th>Project</th>
              <th>Host</th>
              <th>Last active</th>
              <th>Duration</th>
              <th>Processes</th>
              <th>Alerts</th>
              <th>Tokens</th>
              <th>Session</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((s) => (
              <tr key={`${s.host_id}/${s.agent_session_id}`}>
                <td>
                  <Link to={sessionHref(s)}>{s.agent_name}</Link>
                  {s.agent_version ? ` ${s.agent_version}` : ""}
                  {s.bypass && (
                    <>
                      {" "}
                      <span title={BYPASS_TITLE}><Badge variant="high">Bypass permissions</Badge></span>
                    </>
                  )}
                </td>
                <td className="search-page__mono">{s.cwd ?? ""}</td>
                <td>{hostNames.get(s.host_id) ?? s.host_id}</td>
                <td className="search-page__nowrap" title={formatNs(s.last_seen_ns)}>{formatRelativeNs(s.last_seen_ns)}</td>
                <td className="search-page__nowrap" title={`${formatNs(s.first_seen_ns)} to ${formatNs(s.last_seen_ns)}`}>
                  {formatElapsedNs(s.last_seen_ns - s.first_seen_ns)}
                </td>
                <td>{s.process_count.toLocaleString()}</td>
                <td>{s.alert_count.toLocaleString()}</td>
                <td title={s.tokens === undefined ? undefined : `${s.tokens.toLocaleString()} tokens`}>
                  {s.tokens === undefined ? "" : formatTokens(s.tokens)}
                </td>
                <td className="search-page__mono search-page__nowrap" title={s.agent_session_id}>{s.agent_session_id.slice(0, SESSION_ID_PREFIX)}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      </SearchResultsFrame>
    </div>
  );
}

// sessionHref opens the session's own view.
function sessionHref(s: AgentSessionSummary): string {
  return sessionPath(s.host_id, s.agent_session_id);
}
