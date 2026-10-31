package mysql

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/rawtrail/rawtrail/server/detection/api"
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// sessionCursor is the keyset position of the sessions list: the last row of the previous page by its sort key. Session ids are the
// agent's own strings and can hold any separator, so the cursor is JSON rather than a delimited pair.
type sessionCursor struct {
	LastSeenNs int64  `json:"l"`
	HostID     string `json:"h"`
	SessionID  string `json:"s"`
}

func encodeSessionCursor(c sessionCursor) string {
	raw, err := json.Marshal(c)
	if err != nil {
		panic(err) // unreachable: a struct of an int and two strings always marshals
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeSessionCursor(token string) (sessionCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return sessionCursor{}, fmt.Errorf("%w: not base64url: %w", api.ErrInvalidCursor, err)
	}
	var c sessionCursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return sessionCursor{}, fmt.Errorf("%w: not a session cursor: %w", api.ErrInvalidCursor, err)
	}
	// JSON leaves a missing member at its zero value, and no row has a zero last-seen or an empty host or session, so a cursor
	// missing any of them names no position: reject it rather than page from nowhere.
	if c.LastSeenNs <= 0 || c.HostID == "" || c.SessionID == "" {
		return sessionCursor{}, fmt.Errorf("%w: incomplete session cursor", api.ErrInvalidCursor)
	}
	return c, nil
}

// GetAgentSession reads one session's record without its process and alert counts, which a caller that needs only the agent's pid
// or the session's time span should not pay for (issue #1334). It returns nil when the host holds no such session.
func (s *Store) GetAgentSession(ctx context.Context, hostID, sessionID string) (*api.AgentSessionSummary, error) {
	var rows []api.AgentSessionSummary
	if err := s.db.SelectContext(ctx, &rows, `
		SELECT s.host_id, s.agent_session_id, s.agent_name, s.agent_version, s.agent_pid, s.cwd, s.first_seen_ns, s.last_seen_ns,
			(SELECT p.uid FROM processes p WHERE p.host_id = s.host_id AND p.agent_session_id = s.agent_session_id AND p.uid IS NOT NULL
			 ORDER BY p.fork_time_ns, p.id LIMIT 1) AS owner_uid
		FROM agent_sessions s WHERE s.host_id = ? AND s.agent_session_id = ?`, hostID, sessionID,
	); err != nil {
		return nil, fmt.Errorf("get agent session: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// AgentSessionCounts counts one session's retained processes and the alerts raised on them by severity (issue #1422), as the sessions
// list counts them, both served by idx_processes_host_agent_session_fork.
func (s *Store) AgentSessionCounts(ctx context.Context, hostID, sessionID string) (api.AgentSessionCounts, error) {
	out := api.AgentSessionCounts{AlertsBySeverity: map[string]int64{}}
	if err := s.db.GetContext(ctx, &out.Processes,
		`SELECT COUNT(*) FROM processes WHERE host_id = ? AND agent_session_id = ?`, hostID, sessionID); err != nil {
		return api.AgentSessionCounts{}, fmt.Errorf("count session processes: %w", err)
	}
	var bySeverity []struct {
		Severity string `db:"severity"`
		N        int64  `db:"n"`
	}
	if err := s.db.SelectContext(ctx, &bySeverity, `
		SELECT a.severity, COUNT(*) AS n FROM alerts a JOIN processes p ON p.id = a.process_id
		WHERE p.host_id = ? AND p.agent_session_id = ? AND a.disposition = ?
		GROUP BY a.severity`, hostID, sessionID, api.AlertDispositionAlert); err != nil {
		return api.AgentSessionCounts{}, fmt.Errorf("count session alerts by severity: %w", err)
	}
	for _, c := range bySeverity {
		out.AlertsBySeverity[c.Severity] = c.N
	}
	return out, nil
}

// sessionOwnerUID is an agent_sessions row's owner: the uid of its earliest retained process, which the kernel stamped (issue #1478).
// NULL once the session's processes have aged out.
const sessionOwnerUID = `(SELECT p.uid FROM processes p
	WHERE p.host_id = agent_sessions.host_id AND p.agent_session_id = agent_sessions.agent_session_id AND p.uid IS NOT NULL
	ORDER BY p.fork_time_ns, p.id LIMIT 1)`

// sessionBypassed says an agent_sessions row's session ran in the bypassPermissions mode (issue #1427), by a line from its owner's
// transcript (issue #1478), a point lookup on agent_session_bypass's primary key. A session with no owner known is not marked.
const sessionBypassed = `EXISTS (SELECT 1 FROM agent_session_bypass b
	WHERE b.host_id = agent_sessions.host_id AND b.agent_session_id = agent_sessions.agent_session_id AND b.uid = ` +
	sessionOwnerUID + `)`

// AlertedProcesses lists which of the given process rows an alert, not a monitor record, was raised on (issue #1443), in chunks of
// processUpdateChunk rows. A command's processes include what launchd started for it, which carries no session marker, so they are
// asked for by row rather than by session.
func (s *Store) AlertedProcesses(ctx context.Context, processIDs []int64) ([]int64, error) {
	var out []int64
	for start := 0; start < len(processIDs); start += processUpdateChunk {
		chunk := processIDs[start:min(start+processUpdateChunk, len(processIDs))]
		query, args, err := sqlx.In(`SELECT DISTINCT process_id FROM alerts WHERE process_id IN (?) AND disposition = ?`,
			chunk, api.AlertDispositionAlert)
		if err != nil {
			return nil, fmt.Errorf("build alerted processes query: %w", err)
		}
		var ids []int64
		if err := s.db.SelectContext(ctx, &ids, s.db.Rebind(query), args...); err != nil {
			return nil, fmt.Errorf("list alerted processes: %w", err)
		}
		out = append(out, ids...)
	}
	return out, nil
}

// ListAgentSessions returns one page of agent sessions (issue #1283), most recently active first, keyset-paged on (last_seen_ns,
// host_id, agent_session_id), which is idx_agent_sessions_last_seen's order (InnoDB appends the primary key), so a page reads only
// its rows. Each row carries its process and alert counts, read with one grouped query each over the page's sessions rather than one
// per row.
func (s *Store) ListAgentSessions(
	ctx context.Context, filter api.AgentSessionFilter, cursor string, limit int,
) (api.AgentSessionResult, error) {
	limit = max(limit, 1)
	where, args := []string{"1=1"}, []any{}
	if filter.HostID != "" {
		where, args = append(where, "host_id = ?"), append(args, filter.HostID)
	}
	if filter.AgentSessionID != "" {
		where, args = append(where, "agent_session_id = ?"), append(args, filter.AgentSessionID)
	}
	if filter.AgentName != "" {
		where, args = append(where, "agent_name = ?"), append(args, filter.AgentName)
	}
	if filter.FromNs > 0 {
		where, args = append(where, "last_seen_ns >= ?"), append(args, filter.FromNs)
	}
	if filter.ToNs > 0 {
		where, args = append(where, "first_seen_ns <= ?"), append(args, filter.ToNs)
	}
	if filter.Bypass {
		where = append(where, sessionBypassed)
	}
	var total int64
	if err := s.db.GetContext(ctx, &total, "SELECT COUNT(*) FROM agent_sessions WHERE "+strings.Join(where, " AND "), args...); err != nil {
		return api.AgentSessionResult{}, fmt.Errorf("count agent sessions: %w", err)
	}
	if cursor != "" {
		c, err := decodeSessionCursor(cursor)
		if err != nil {
			return api.AgentSessionResult{}, err
		}
		where, args = append(where, "(last_seen_ns, host_id, agent_session_id) < (?, ?, ?)"), append(args, c.LastSeenNs, c.HostID, c.SessionID)
	}
	var rows []api.AgentSessionSummary
	if err := s.db.SelectContext(ctx, &rows, `
		SELECT host_id, agent_session_id, agent_name, agent_version, agent_pid, cwd, first_seen_ns, last_seen_ns,
			`+sessionOwnerUID+` AS owner_uid, `+sessionBypassed+` AS bypass
		FROM agent_sessions WHERE `+strings.Join(where, " AND ")+`
		ORDER BY last_seen_ns DESC, host_id DESC, agent_session_id DESC
		LIMIT ?`, append(args, limit+1)...,
	); err != nil {
		return api.AgentSessionResult{}, fmt.Errorf("list agent sessions: %w", err)
	}
	result := api.AgentSessionResult{TotalMatched: total}
	if len(rows) > limit {
		last := rows[limit-1]
		result.NextCursor = encodeSessionCursor(sessionCursor{LastSeenNs: last.LastSeenNs, HostID: last.HostID, SessionID: last.AgentSessionID})
		rows = rows[:limit]
	}
	if err := s.countSessionActivity(ctx, rows); err != nil {
		return api.AgentSessionResult{}, err
	}
	s.fillSessionTokens(ctx, rows)
	result.Rows = rows
	return result, nil
}

// sessionTokensTimeout bounds the sessions list's token read, so a stalled event archive delays the list by seconds rather than
// holding it past the server's write timeout.
const sessionTokensTimeout = 3 * time.Second

// fillSessionTokens fills each row's token total from the event archive with one read over the page's sessions. The list is served
// from MySQL, so an archive that cannot be read in time leaves the totals out rather than failing the list.
func (s *Store) fillSessionTokens(ctx context.Context, rows []api.AgentSessionSummary) {
	if len(rows) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, sessionTokensTimeout)
	defer cancel()
	// A session's tokens are counted from its owner's transcripts (issue #1478); a session whose owner is no longer known has none.
	refs := make([]visibilityapi.AgentSessionRef, 0, len(rows))
	at := make([]int, 0, len(rows))
	for i, r := range rows {
		if r.OwnerUID != nil {
			refs = append(refs, visibilityapi.AgentSessionRef{HostID: r.HostID, SessionID: r.AgentSessionID, OwnerUID: *r.OwnerUID})
			at = append(at, i)
		}
	}
	if len(refs) == 0 {
		return
	}
	tokens, err := s.archive.AgentSessionTokens(ctx, refs)
	if err != nil {
		s.logger.WarnContext(ctx, "agent sessions list: token totals unavailable", "err", err)
		return
	}
	for k, ref := range refs {
		if n, ok := tokens[ref]; ok {
			rows[at[k]].Tokens = &n
		}
	}
}

// countSessionActivity fills each row's process and alert counts with one grouped query apiece over the page's sessions, both served
// by idx_processes_host_agent_session_fork.
func (s *Store) countSessionActivity(ctx context.Context, rows []api.AgentSessionSummary) error {
	if len(rows) == 0 {
		return nil
	}
	keys := make([]string, 0, len(rows))
	args := make([]any, 0, len(rows)*2)
	index := make(map[[2]string]int, len(rows))
	for i, r := range rows {
		keys = append(keys, "(?, ?)")
		args = append(args, r.HostID, r.AgentSessionID)
		index[[2]string{r.HostID, r.AgentSessionID}] = i
	}
	in := "(" + strings.Join(keys, ", ") + ")"
	type count struct {
		HostID    string `db:"host_id"`
		SessionID string `db:"agent_session_id"`
		N         int64  `db:"n"`
	}
	var processes, alerts []count
	if err := s.db.SelectContext(ctx, &processes, `
		SELECT host_id, agent_session_id, COUNT(*) AS n FROM processes
		WHERE (host_id, agent_session_id) IN `+in+` GROUP BY host_id, agent_session_id`, args...); err != nil {
		return fmt.Errorf("count session processes: %w", err)
	}
	if err := s.db.SelectContext(ctx, &alerts, `
		SELECT p.host_id, p.agent_session_id, COUNT(*) AS n FROM alerts a JOIN processes p ON p.id = a.process_id
		WHERE (p.host_id, p.agent_session_id) IN `+in+` AND a.disposition = ?
		GROUP BY p.host_id, p.agent_session_id`, append(args, api.AlertDispositionAlert)...); err != nil {
		return fmt.Errorf("count session alerts: %w", err)
	}
	for _, c := range processes {
		rows[index[[2]string{c.HostID, c.SessionID}]].ProcessCount = c.N
	}
	for _, c := range alerts {
		rows[index[[2]string{c.HostID, c.SessionID}]].AlertCount = c.N
	}
	return nil
}
