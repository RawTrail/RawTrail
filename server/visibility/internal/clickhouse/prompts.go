package clickhouse

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/rawtrail/rawtrail/server/visibility/api"
)

// PromptCursor is a prompt's place in its session's order: its time and event id, which together order the prompts totally. It is
// exported for the in-memory archive, so the two encode one cursor.
type PromptCursor struct {
	AtNs    int64  `json:"t"`
	EventID string `json:"e"`
}

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
		args = append(args, after.AtNs, after.EventID)
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
	var last PromptCursor
	for rows.Next() {
		if len(page.Prompts) == filter.Limit {
			page.NextCursor = EncodePromptCursor(last)
			break
		}
		var p api.AgentPrompt
		var eventID string
		if err := rows.Scan(&eventID, &p.PromptID, &p.Text, &p.Length, &p.Truncated, &p.Redacted, &p.Source, &p.AtNs, &p.UID); err != nil {
			return api.AgentPromptPage{}, fmt.Errorf("scan agent prompt: %w", err)
		}
		page.Prompts = append(page.Prompts, p)
		last = PromptCursor{AtNs: p.AtNs, EventID: eventID}
	}
	if err := rows.Err(); err != nil {
		return api.AgentPromptPage{}, fmt.Errorf("iterate agent prompts: %w", err)
	}
	return page, nil
}

// EncodePromptCursor is the opaque token of a prompt's place.
func EncodePromptCursor(c PromptCursor) string {
	raw, err := json.Marshal(c)
	if err != nil {
		panic(err) // unreachable: a struct of an integer and a string always marshals
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodePromptCursor reads a token EncodePromptCursor wrote, and is an api.ErrInvalidPromptCursor for any other.
func DecodePromptCursor(token string) (PromptCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return PromptCursor{}, fmt.Errorf("%w: not base64url", api.ErrInvalidPromptCursor)
	}
	var c PromptCursor
	if err := json.Unmarshal(raw, &c); err != nil || c.EventID == "" {
		return PromptCursor{}, fmt.Errorf("%w: not a prompt cursor", api.ErrInvalidPromptCursor)
	}
	return c, nil
}
