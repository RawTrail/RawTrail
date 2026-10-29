package clickhouse

import (
	"context"
	"fmt"
	"math"

	"github.com/rawtrail/rawtrail/server/visibility/api"
)

// usageMessageID is an agent_usage event's message id, which tells a message's entries apart as tool_use_id does a call's.
const usageMessageID = "JSONExtractString(payload, 'message_id')"

// notCopiedUsageSQL keeps a session's messages that are not copies of another session's, as notCopiedSQL does its calls.
var notCopiedUsageSQL = notCopied("agent_usage", usageMessageID)

// AgentUsageTotals totals one session's message usage by model and counts its messages by permission mode (issue #1427), each
// message once by its id and owner and the copies of another session's left out. Both reads are over the same messages.
func (s *Store) AgentUsageTotals(ctx context.Context, filter api.AgentToolCallFilter) (api.AgentUsageTotals, error) {
	// messages is one row per message and owner, at its first recording: a re-read transcript sends the same message again.
	messages := `
		SELECT ` + usageMessageID + ` AS id, ` + callUID + ` AS uid,
			argMin((JSONExtractString(payload, 'model'), JSONExtractInt(payload, 'input_tokens'), JSONExtractInt(payload, 'output_tokens'),
				JSONExtractInt(payload, 'cache_creation_input_tokens'), JSONExtractInt(payload, 'cache_read_input_tokens'),
				JSONExtractInt(payload, 'thinking_tokens'), JSONExtractString(payload, 'permission_mode')), (timestamp_ns, event_id)) AS m,
			min(timestamp_ns) AS at
		FROM events FINAL
		WHERE ` + hostPredicate + ` AND event_type = 'agent_usage' AND ` + callSession + ` = ? AND ` + notCopiedUsageSQL + `
		GROUP BY id, uid`
	args := []any{filter.HostID, filter.SessionID, filter.HostID, filter.HostID, filter.SessionID, filter.HostID, filter.HostID,
		filter.SessionID}
	var out api.AgentUsageTotals
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.1 AS model, count(), sum(m.2), sum(m.3), sum(m.4), sum(m.5), sum(m.6)
		FROM (`+messages+`)
		GROUP BY model
		ORDER BY sum(m.3) DESC, model`, args...)
	if err != nil {
		return api.AgentUsageTotals{}, fmt.Errorf("clickhouse agent usage by model: %w", err)
	}
	if out.Models, _, err = scanActivity(rows, math.MaxInt, func(u *api.ModelUsage) []any {
		return []any{&u.Model, &u.Messages, &u.Input, &u.Output, &u.CacheCreation, &u.CacheRead, &u.Thinking}
	}); err != nil {
		return api.AgentUsageTotals{}, err
	}
	rows, err = s.db.QueryContext(ctx, `
		SELECT m.7 AS mode, count(), min(at), max(at)
		FROM (`+messages+`)
		GROUP BY mode
		ORDER BY min(at), mode`, args...)
	if err != nil {
		return api.AgentUsageTotals{}, fmt.Errorf("clickhouse agent usage by mode: %w", err)
	}
	if out.Modes, _, err = scanActivity(rows, math.MaxInt, func(u *api.ModeUsage) []any {
		return []any{&u.Mode, &u.Messages, &u.FirstNs, &u.LastNs}
	}); err != nil {
		return api.AgentUsageTotals{}, err
	}
	return out, nil
}
