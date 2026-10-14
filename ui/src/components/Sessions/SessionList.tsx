import { useCallback, useRef } from "react";
import { Link } from "react-router";
import "../Search/SearchPage.scss";
import { listAgentSessions, type AgentSessionFilter } from "../../api";
import type { AgentSessionSummary } from "../../types";
import { Table } from "../ui/Table";
import { FilterChips, type FilterField } from "../Search/FilterChips";
import { useCursorList, type CursorPage } from "../Search/useCursorList";
import { useUrlFilter } from "../Search/useUrlFilter";
import { SearchResultsFrame } from "../Search/SearchResultsFrame";
import { formatNs } from "../Search/format";
import { useHostNames } from "../useHostNames";
import { MILLISECONDS_PER_DAY, NANOSECONDS_PER_MILLISECOND } from "../../constants";

// ACTIVE_WINDOWS are the "active in the last" choices, by the URL value the chip stores. The URL keeps the relative choice rather than
// absolute bounds so a bookmarked list stays "the last week" when it is reopened.
const DAYS_PER_WEEK = 7;
const DAYS_PER_MONTH = 30;
const ACTIVE_WINDOWS: Record<string, number> = {
  "1d": MILLISECONDS_PER_DAY,
  "7d": DAYS_PER_WEEK * MILLISECONDS_PER_DAY,
  "30d": DAYS_PER_MONTH * MILLISECONDS_PER_DAY,
};
// SESSION_ID_PREFIX is how much of a session id the table shows; the full id is the cell's title.
const SESSION_ID_PREFIX = 8;

const FILTER_FIELDS: FilterField[] = [
  { key: "host_id", label: "Host" },
  { key: "agent", label: "Agent" },
  {
    key: "active",
    label: "Active in",
    options: [
      { value: "1d", label: "Last 24 hours" },
      { value: "7d", label: "Last 7 days" },
      { value: "30d", label: "Last 30 days" },
    ],
  },
];

const FILTER_KEYS = FILTER_FIELDS.map((f) => f.key);

// sessionFilter turns the URL's filter into the endpoint's. The active window resolves against the clock when the list is (re)loaded,
// so a reload after the page has been open a while still means "the last week".
function sessionFilter(active: Record<string, string>): AgentSessionFilter {
  const filter: AgentSessionFilter = { host_id: active.host_id, agent: active.agent };
  const windowMs = ACTIVE_WINDOWS[active.active] as number | undefined;
  if (windowMs !== undefined) filter.from = String((Date.now() - windowMs) * NANOSECONDS_PER_MILLISECOND);
  return filter;
}

// SessionList is the AI agent sessions page (issue #1283): every session the fleet's agents ran, most recently active first, with the
// project each worked in and how much it did. Filters live in the URL and are applied by the server, which also pages the list.
export function SessionList() {
  const { activeFilter, filterKey, setFilter } = useUrlFilter(FILTER_KEYS);
  const hostNames = useHostNames();

  // The first page resolves the filter, and every later page reuses it: a cursor names a position in one result set, so "Load more"
  // must not move the active window's start to a later clock reading.
  const resolvedFilter = useRef<AgentSessionFilter>({});
  const fetchPage = useCallback(
    async (cursor: string): Promise<CursorPage<AgentSessionSummary>> => {
      if (!cursor) resolvedFilter.current = sessionFilter(activeFilter);
      const res = await listAgentSessions(resolvedFilter.current, cursor || undefined);
      return { rows: res.rows, nextCursor: res.next_cursor, total: res.total_matched };
    },
    [filterKey], // eslint-disable-line react-hooks/exhaustive-deps -- activeFilter is captured; filterKey is its stable identity
  );
  const { rows, total, loading, error, hasMore, loadMore } = useCursorList(filterKey, fetchPage);

  return (
    <div className="search-page">
      <FilterChips fields={FILTER_FIELDS} active={activeFilter} onChange={setFilter} />
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
              <th>First seen</th>
              <th>Last seen</th>
              <th>Processes</th>
              <th>Alerts</th>
              <th>Session</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((s) => (
              <tr key={`${s.host_id}/${s.agent_session_id}`}>
                <td>
                  <Link to={sessionHref(s)}>{s.agent_name}</Link>
                  {s.agent_version ? ` ${s.agent_version}` : ""}
                </td>
                <td className="search-page__mono">{s.cwd ?? ""}</td>
                <td>{hostNames.get(s.host_id) ?? s.host_id}</td>
                <td>{formatNs(s.first_seen_ns)}</td>
                <td>{formatNs(s.last_seen_ns)}</td>
                <td>{s.process_count.toLocaleString()}</td>
                <td>{s.alert_count.toLocaleString()}</td>
                <td className="search-page__mono" title={s.agent_session_id}>{s.agent_session_id.slice(0, SESSION_ID_PREFIX)}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      </SearchResultsFrame>
    </div>
  );
}

// sessionHref opens the session's host on a window ending just after the session's last activity: the host page's ?at= anchor ends
// its window there. It is the next millisecond rather than a rounding up, because a current-epoch nanosecond time loses its low digits
// as a JSON number and can read as earlier than the event. The session view (issue #1284) replaces this target.
function sessionHref(s: AgentSessionSummary): string {
  const atMs = Math.floor(s.last_seen_ns / NANOSECONDS_PER_MILLISECOND) + 1;
  return `/hosts/${encodeURIComponent(s.host_id)}?at=${String(atMs)}`;
}
