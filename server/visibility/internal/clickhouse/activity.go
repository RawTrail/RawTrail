package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/fleetdm/edr/server/visibility/api"
)

// defaultActivityLimit is how many rows each class lists when the caller names no limit.
const defaultActivityLimit = 200

// Activity SQL fragments (issue #1285). A coalesced flow stands for coalesced_count connections or queries, and an agent's folded file
// activity (#1287) for coalesced_count operations, so each weighs that much in every count; an event without the field is one. A
// coalesced event's last occurrence is last_timestamp_ns, later than its own time. An agent file row's operation is its payload's
// (create, modify, rename, delete); every other file event's is its type.
const (
	activityFileTypes     = "('open', 'file_rename', 'file_truncate', 'file_delete', 'file_activity')"
	activityAllTypes      = "('open', 'file_rename', 'file_truncate', 'file_delete', 'file_activity', 'network_connect', 'dns_query')"
	activityFileOperation = "if(event_type = 'file_activity', JSONExtractString(payload, 'operation'), event_type)"
	activityWeight        = "if(JSONHas(payload, 'coalesced_count'), JSONExtractInt(payload, 'coalesced_count'), 1)"
	activityLastTimestamp = "greatest(timestamp_ns, JSONExtractInt(payload, 'last_timestamp_ns'))"
	activityPIDVersion    = "JSONExtractInt(payload, 'pidversion')"
)

// ActivityForGenerations rolls up what the generations touched: one aggregate for every class's totals, then each class's rows grouped
// by artifact and generation, oldest first. FINAL collapses ReplacingMergeTree duplicates so a re-delivered event is not counted
// twice. Each class reads one row past the limit to report that it was cut.
func (s *Store) ActivityForGenerations(ctx context.Context, filter api.ActivityFilter) (api.ActivityRollup, error) {
	out := api.ActivityRollup{
		Files:   api.FileActivity{Rows: []api.FileActivityRow{}},
		Network: api.NetworkActivity{Rows: []api.NetworkActivityRow{}},
		DNS:     api.DNSActivity{Rows: []api.DNSActivityRow{}},
	}
	if len(filter.Generations) == 0 && len(filter.Spans) == 0 {
		return out, nil
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultActivityLimit
	}
	genClause, genArgs := activityScopeSQL(filter)
	scope := hostPredicate + " AND " + genClause
	scopeArgs := append([]any{filter.HostID}, genArgs...)

	if err := s.activityTotals(ctx, scope, scopeArgs, &out); err != nil {
		return api.ActivityRollup{}, err
	}
	var err error
	if out.Files.Rows, out.Files.Truncated, err = s.fileActivity(ctx, scope, scopeArgs, limit); err != nil {
		return api.ActivityRollup{}, err
	}
	if out.Network.Rows, out.Network.Truncated, err = s.networkActivity(ctx, scope, scopeArgs, limit); err != nil {
		return api.ActivityRollup{}, err
	}
	if out.DNS.Rows, out.DNS.Truncated, err = s.dnsActivity(ctx, scope, scopeArgs, limit); err != nil {
		return api.ActivityRollup{}, err
	}
	return out, nil
}

// activityTotals counts each class's events and distinct artifacts in one pass: a file's artifact is its path, a connection's its
// destination (address, port and protocol), a query's its name.
func (s *Store) activityTotals(ctx context.Context, scope string, args []any, out *api.ActivityRollup) error {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			toInt64(sumIf(`+activityWeight+`, event_type IN `+activityFileTypes+`)),
			toInt64(uniqExactIf(JSONExtractString(payload, 'path'), event_type IN `+activityFileTypes+`)),
			toInt64(sumIf(`+activityWeight+`, event_type = 'network_connect')),
			toInt64(uniqExactIf((JSONExtractString(payload, 'remote_address'), JSONExtractInt(payload, 'remote_port'),
				JSONExtractString(payload, 'protocol')), event_type = 'network_connect')),
			toInt64(sumIf(`+activityWeight+`, event_type = 'dns_query')),
			toInt64(uniqExactIf(JSONExtractString(payload, 'query_name'), event_type = 'dns_query'))
		FROM events FINAL
		WHERE event_type IN `+activityAllTypes+` AND `+scope, args...)
	if err := row.Scan(&out.Files.Events, &out.Files.Distinct, &out.Network.Events, &out.Network.Distinct,
		&out.DNS.Events, &out.DNS.Distinct); err != nil {
		return fmt.Errorf("clickhouse activity totals: %w", err)
	}
	return nil
}

func (s *Store) fileActivity(ctx context.Context, scope string, args []any, limit int) ([]api.FileActivityRow, bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+activityFileOperation+` AS operation, JSONExtractString(payload, 'path') AS path,
			JSONExtractString(payload, 'source_path') AS source_path, toBool(JSONExtractBool(payload, 'rolled_up')) AS rolled_up,
			pid, `+activityPIDVersion+` AS pv, toInt64(sum(`+activityWeight+`)), min(timestamp_ns) AS first, max(`+activityLastTimestamp+`)
		FROM events FINAL
		WHERE event_type IN `+activityFileTypes+` AND `+scope+`
		GROUP BY operation, path, source_path, rolled_up, pid, pv
		ORDER BY first, path, operation
		LIMIT ?`, append(args, limit+1)...)
	if err != nil {
		return nil, false, fmt.Errorf("clickhouse file activity: %w", err)
	}
	return scanActivity(rows, limit, func(r *api.FileActivityRow) []any {
		return []any{&r.Operation, &r.Path, &r.SourcePath, &r.RolledUp, &r.PID, &r.PIDVersion, &r.Count, &r.FirstNs, &r.LastNs}
	})
}

func (s *Store) networkActivity(ctx context.Context, scope string, args []any, limit int) ([]api.NetworkActivityRow, bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT JSONExtractString(payload, 'remote_address') AS address, JSONExtractInt(payload, 'remote_port') AS port,
			JSONExtractString(payload, 'protocol') AS protocol,
			anyIf(JSONExtractString(payload, 'remote_hostname'), JSONExtractString(payload, 'remote_hostname') != ''),
			pid, `+activityPIDVersion+` AS pv, toInt64(sum(`+activityWeight+`)), min(timestamp_ns) AS first,
			max(`+activityLastTimestamp+`)
		FROM events FINAL
		WHERE event_type = 'network_connect' AND `+scope+`
		GROUP BY address, port, protocol, pid, pv
		ORDER BY first, address, port
		LIMIT ?`, append(args, limit+1)...)
	if err != nil {
		return nil, false, fmt.Errorf("clickhouse network activity: %w", err)
	}
	return scanActivity(rows, limit, func(r *api.NetworkActivityRow) []any {
		return []any{&r.RemoteAddress, &r.RemotePort, &r.Protocol, &r.RemoteHostname, &r.PID, &r.PIDVersion, &r.Count, &r.FirstNs, &r.LastNs}
	})
}

// dnsActivity lists the names resolved, each with up to 16 of the addresses its answers carried, sorted. The bound is inside the
// aggregate, so a name whose answers rotate through many addresses does not collect all of them only to keep 16.
func (s *Store) dnsActivity(ctx context.Context, scope string, args []any, limit int) ([]api.DNSActivityRow, bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT JSONExtractString(payload, 'query_name') AS name,
			arraySort(groupUniqArrayArray(16)(JSONExtract(payload, 'response_addresses', 'Array(String)'))),
			pid, `+activityPIDVersion+` AS pv, toInt64(sum(`+activityWeight+`)), min(timestamp_ns) AS first,
			max(`+activityLastTimestamp+`)
		FROM events FINAL
		WHERE event_type = 'dns_query' AND `+scope+`
		GROUP BY name, pid, pv
		ORDER BY first, name
		LIMIT ?`, append(args, limit+1)...)
	if err != nil {
		return nil, false, fmt.Errorf("clickhouse dns activity: %w", err)
	}
	return scanActivity(rows, limit, func(r *api.DNSActivityRow) []any {
		return []any{&r.QueryName, &r.Addresses, &r.PID, &r.PIDVersion, &r.Count, &r.FirstNs, &r.LastNs}
	})
}

// scanActivity reads up to limit rows, each through the destinations fields names, and reports whether a row past the limit was
// there.
func scanActivity[R any](rows *sql.Rows, limit int, fields func(*R) []any) ([]R, bool, error) {
	defer rows.Close()
	out := []R{}
	truncated := false
	for rows.Next() {
		if len(out) == limit {
			truncated = true
			break
		}
		var r R
		if err := rows.Scan(fields(&r)...); err != nil {
			return nil, false, fmt.Errorf("scan activity row: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate activity rows: %w", err)
	}
	return out, truncated, nil
}

// AgentToolCalls lists one session's tool calls (issue #1288). Calls are grouped by tool_use_id and the transcript's owner, so a call
// reported again is listed once, at the time the agent first recorded it, and a line another user forged with the same ids is listed
// apart, under its own uid.
func (s *Store) AgentToolCalls(ctx context.Context, filter api.AgentToolCallFilter) ([]api.AgentToolCall, bool, error) {
	where := hostPredicate + ` AND event_type = 'agent_tool_call' AND JSONExtractString(payload, 'agent_session_id') = ?`
	args := []any{filter.HostID, filter.SessionID}
	if filter.FromNs > 0 {
		where += " AND timestamp_ns >= ?"
		args = append(args, filter.FromNs)
	}
	if filter.ToNs > 0 {
		where += " AND timestamp_ns <= ?"
		args = append(args, filter.ToNs)
	}
	where += " AND NOT (" + copiedCallSQL + ")"
	args = append(args, filter.HostID)
	// One argMin over the whole record, so every field comes from the first recording, as the time does.
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, first.1, first.2, first.3, first.4, toBool(first.5), toBool(first.6), at, uid
		FROM (
			SELECT JSONExtractString(payload, 'tool_use_id') AS id, JSONExtractInt(payload, 'uid') AS uid,
				argMin((JSONExtractString(payload, 'tool_name'), JSONExtractString(payload, 'command'),
					JSONExtractString(payload, 'description'), JSONExtractString(payload, 'file_path'),
					JSONExtractBool(payload, 'sidechain'), JSONExtractBool(payload, 'truncated')), (timestamp_ns, event_id)) AS first,
				min(timestamp_ns) AS at
			FROM events FINAL
			WHERE `+where+`
			GROUP BY id, uid
		)
		ORDER BY at, id, uid
		LIMIT ?`, append(args, filter.Limit+1)...)
	if err != nil {
		return nil, false, fmt.Errorf("clickhouse agent tool calls: %w", err)
	}
	return scanActivity(rows, filter.Limit, func(c *api.AgentToolCall) []any {
		return []any{&c.ToolUseID, &c.ToolName, &c.Command, &c.Description, &c.FilePath, &c.Sidechain, &c.Truncated, &c.CalledAtNs, &c.UID}
	})
}

// copiedCallSQL matches a tool call copied from another session (issue #1347): one naming an origin session that holds the same
// tool_use_id under the same owner. The origin alone is not enough: a process that switched sessions (/clear) writes its first
// session's id on the new session's own calls, which that session never held. Its one argument is the host.
const copiedCallSQL = `JSONExtractString(payload, 'origin_session_id') != '' AND
	(JSONExtractString(payload, 'origin_session_id'), JSONExtractString(payload, 'tool_use_id'), JSONExtractInt(payload, 'uid')) IN (
		SELECT JSONExtractString(payload, 'agent_session_id'), JSONExtractString(payload, 'tool_use_id'), JSONExtractInt(payload, 'uid')
		FROM events FINAL WHERE ` + hostPredicate + ` AND event_type = 'agent_tool_call')`

// AgentSessionLineage reads a session's parent and forks (issue #1347) from the copied calls on its host: each session's parent is the
// origin of its latest copy, and the forks are the sessions holding a copy from this one whose parent it is.
func (s *Store) AgentSessionLineage(ctx context.Context, hostID, sessionID string) (api.AgentSessionLineage, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH copies AS (
			SELECT JSONExtractString(payload, 'agent_session_id') AS session, JSONExtractString(payload, 'origin_session_id') AS origin,
				timestamp_ns, event_id
			FROM events FINAL
			WHERE `+hostPredicate+` AND event_type = 'agent_tool_call' AND `+copiedCallSQL+`
		)
		SELECT session, argMax(origin, (timestamp_ns, event_id)) AS parent
		FROM copies
		WHERE session = ? OR session IN (SELECT session FROM copies WHERE origin = ?)
		GROUP BY session
		ORDER BY session`, hostID, hostID, sessionID, sessionID)
	if err != nil {
		return api.AgentSessionLineage{}, fmt.Errorf("clickhouse agent session lineage: %w", err)
	}
	defer rows.Close()
	lineage := api.AgentSessionLineage{ForkedSessionIDs: []string{}}
	for rows.Next() {
		var session, parent string
		if err := rows.Scan(&session, &parent); err != nil {
			return api.AgentSessionLineage{}, fmt.Errorf("scan agent session lineage: %w", err)
		}
		switch {
		case session == sessionID:
			lineage.ForkedFromSessionID = parent
		case parent == sessionID:
			lineage.ForkedSessionIDs = append(lineage.ForkedSessionIDs, session)
		}
	}
	if err := rows.Err(); err != nil {
		return api.AgentSessionLineage{}, fmt.Errorf("iterate agent session lineage: %w", err)
	}
	return lineage, nil
}

// activityScopeSQL matches the filter's generations: those in Generations wholly, those in Spans only inside their span (issue
// #1319).
func activityScopeSQL(filter api.ActivityFilter) (string, []any) {
	var arms []string
	var args []any
	if len(filter.Generations) > 0 {
		clause, genArgs := generationIdentitySQL(filter.Generations)
		arms = append(arms, clause)
		args = append(args, genArgs...)
	}
	for _, span := range filter.Spans {
		clause, genArgs := generationIdentitySQL([]api.ProcessGeneration{span.ProcessGeneration})
		clause += " AND timestamp_ns >= ?"
		genArgs = append(genArgs, span.FromNs)
		if span.ToNs > 0 {
			clause += " AND timestamp_ns < ?"
			genArgs = append(genArgs, span.ToNs)
		}
		arms = append(arms, "("+clause+")")
		args = append(args, genArgs...)
	}
	return "(" + strings.Join(arms, " OR ") + ")", args
}

// SessionPathPattern matches a path Claude Code names after a session, and captures the session's id: its transcript,
// ~/.claude/projects/<project>/<session id>.jsonl, and the session's directories, ~/.claude/projects/<project>/<session id>/,
// ~/.claude/session-env/<session id> and /tmp/claude-<uid>/<project>/<session id>/. Claude Code creates the scratch directory a few
// milliseconds before the transcript, so the transcript alone would leave those first writes in the previous session.
const SessionPathPattern = `(?:/\.claude/projects/[^/]+/|/\.claude/session-env/|/claude-[0-9]+/[^/]+/)` +
	`([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(?:\.jsonl$|/|$)`

// SessionTouches lists the first time the generation created or changed a path of each session, oldest first (issue #1319).
func (s *Store) SessionTouches(ctx context.Context, hostID string, generation api.ProcessGeneration) ([]api.SessionTouch, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT extract(JSONExtractString(payload, 'path'), ?) AS session, min(timestamp_ns) AS first
		FROM events FINAL
		WHERE `+hostPredicate+` AND event_type = 'file_activity' AND pid = ? AND `+activityPIDVersion+` = ?
			AND match(JSONExtractString(payload, 'path'), ?)
		GROUP BY session
		ORDER BY first, session`, SessionPathPattern, hostID, generation.PID, generation.PIDVersion,
		SessionPathPattern)
	if err != nil {
		return nil, fmt.Errorf("clickhouse session touches: %w", err)
	}
	defer rows.Close()
	touches := []api.SessionTouch{}
	for rows.Next() {
		var t api.SessionTouch
		if err := rows.Scan(&t.SessionID, &t.FirstNs); err != nil {
			return nil, fmt.Errorf("scan session touch: %w", err)
		}
		touches = append(touches, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session touches: %w", err)
	}
	return touches, nil
}
