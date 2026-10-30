package clickhouse

import (
	"context"
	"fmt"
	"strings"

	"github.com/rawtrail/rawtrail/server/visibility/api"
)

// AgentPrompts reads a page of one session's prompts (issue #1426), oldest first, after the filter's cursor. The prompts are the
// session's own: the agent sends a prompt only from a transcript of the session it names, so a fork's copy of its parent's history
// never arrives as this session's.
func (s *Store) AgentPrompts(ctx context.Context, filter api.AgentPromptFilter) (api.AgentPromptPage, error) {
	where := hostPredicate + ` AND event_type = 'agent_prompt' AND JSONExtractString(payload, 'agent_session_id') = ?`
	args := []any{filter.HostID, filter.SessionID}
	if filter.Cursor != "" {
		after, err := DecodePromptCursor(filter.Cursor)
		if err != nil {
			return api.AgentPromptPage{}, err
		}
		where += " AND (timestamp_ns, event_id) > (?, ?)"
		args = append(args, after.TimestampNs, after.EventID)
	}
	if filter.Search != "" {
		where += " AND positionCaseInsensitiveUTF8(JSONExtractString(payload, 'text'), ?) > 0"
		args = append(args, filter.Search)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT event_id, JSONExtractString(payload, 'prompt_id'), JSONExtractString(payload, 'text'), JSONExtractInt(payload, 'length'),
			JSONExtractBool(payload, 'truncated'), JSONExtractInt(payload, 'redacted'), JSONExtractString(payload, 'source'), timestamp_ns,
			JSONExtractInt(payload, 'uid')
		FROM events FINAL
		WHERE `+where+`
		ORDER BY timestamp_ns, event_id
		LIMIT ?`, append(args, filter.Limit+1)...)
	if err != nil {
		return api.AgentPromptPage{}, fmt.Errorf("clickhouse agent prompts: %w", err)
	}
	defer rows.Close()
	page := api.AgentPromptPage{Prompts: []api.AgentPrompt{}}
	var last api.EventCursor
	for rows.Next() {
		if len(page.Prompts) == filter.Limit {
			page.NextCursor = api.EncodeEventCursor(last)
			break
		}
		var p api.AgentPrompt
		var eventID string
		if err := rows.Scan(&eventID, &p.PromptID, &p.Text, &p.Length, &p.Truncated, &p.Redacted, &p.Source, &p.AtNs, &p.UID); err != nil {
			return api.AgentPromptPage{}, fmt.Errorf("scan agent prompt: %w", err)
		}
		page.Prompts = append(page.Prompts, p)
		last = api.EventCursor{TimestampNs: p.AtNs, EventID: eventID}
	}
	if err := rows.Err(); err != nil {
		return api.AgentPromptPage{}, fmt.Errorf("iterate agent prompts: %w", err)
	}
	return page, nil
}

// SearchAgentPrompts finds prompts by their text across sessions (issue #1428), newest first. It reads the prompt_text column, the
// prompt's text lowercased, of the agent_prompt rows in the window, which the primary key narrows the read to. It reads under FINAL
// with no skip index, so a prompt whose text a redelivery changed is matched by its newest text only.
func (s *Store) SearchAgentPrompts(ctx context.Context, search api.AgentPromptSearch) (api.FoundAgentPromptPage, error) {
	where := `event_type = 'agent_prompt' AND timestamp_ns >= ? AND timestamp_ns <= ? AND prompt_text LIKE ?`
	args := []any{search.FromNs, search.ToNs, "%" + escapeLike(strings.ToLower(search.Query)) + "%"}
	if search.HostID != "" {
		where += " AND " + hostPredicate
		args = append(args, search.HostID)
	}
	if search.Cursor != "" {
		before, err := DecodePromptCursor(search.Cursor)
		if err != nil {
			return api.FoundAgentPromptPage{}, err
		}
		where += " AND (timestamp_ns, event_id) < (?, ?)"
		args = append(args, before.TimestampNs, before.EventID)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT event_id, host_id, JSONExtractString(payload, 'agent_session_id'), JSONExtractString(payload, 'prompt_id'),
			JSONExtractString(payload, 'text'), JSONExtractInt(payload, 'length'), JSONExtractBool(payload, 'truncated'),
			JSONExtractInt(payload, 'redacted'), JSONExtractString(payload, 'source'), timestamp_ns, JSONExtractInt(payload, 'uid')
		FROM events FINAL
		WHERE `+where+`
		ORDER BY timestamp_ns DESC, event_id DESC
		LIMIT ?`, append(args, search.Limit+1)...)
	if err != nil {
		return api.FoundAgentPromptPage{}, fmt.Errorf("clickhouse search agent prompts: %w", err)
	}
	defer rows.Close()
	page := api.FoundAgentPromptPage{Prompts: []api.FoundAgentPrompt{}}
	var last api.EventCursor
	for rows.Next() {
		if len(page.Prompts) == search.Limit {
			page.NextCursor = api.EncodeEventCursor(last)
			break
		}
		var p api.FoundAgentPrompt
		var eventID string
		if err := rows.Scan(&eventID, &p.HostID, &p.SessionID, &p.PromptID, &p.Text, &p.Length, &p.Truncated, &p.Redacted, &p.Source,
			&p.AtNs, &p.UID); err != nil {
			return api.FoundAgentPromptPage{}, fmt.Errorf("scan found agent prompt: %w", err)
		}
		page.Prompts = append(page.Prompts, p)
		last = api.EventCursor{TimestampNs: p.AtNs, EventID: eventID}
	}
	if err := rows.Err(); err != nil {
		return api.FoundAgentPromptPage{}, fmt.Errorf("iterate found agent prompts: %w", err)
	}
	return page, nil
}

// likeEscaper escapes the characters LIKE gives a meaning, so a query is matched as the text it is.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// escapeLike is text as a LIKE pattern that matches it literally.
func escapeLike(text string) string { return likeEscaper.Replace(text) }

// DecodePromptCursor reads a prompt page's cursor, the event search's own (timestamp_ns, event_id) token, and is an
// api.ErrInvalidPromptCursor for anything else. Exported for the in-memory archive, so the two read one cursor.
func DecodePromptCursor(token string) (api.EventCursor, error) {
	c, err := api.DecodeEventCursor(token)
	if err != nil || c.EventID == "" {
		return api.EventCursor{}, fmt.Errorf("%w: %q", api.ErrInvalidPromptCursor, token)
	}
	return c, nil
}
