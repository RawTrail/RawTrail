package clickhouse

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/rawtrail/rawtrail/server/visibility/api"
)

// usageMessageID is an agent_usage event's message id, which tells a message's entries apart as tool_use_id does a call's.
const usageMessageID = "JSONExtractString(payload, 'message_id')"

// notCopiedUsageSQL keeps a session's messages that are not copies of another session's, as notCopiedSQL does its calls.
var notCopiedUsageSQL = notCopied("agent_usage", usageMessageID)

// AgentUsageTotals totals one session's message usage by model and counts its messages by permission mode (issue #1427), each
// message once by its id and owner and the copies of another session's left out. One read groups the messages by model and mode,
// and both breakdowns are folded from it, so they describe the same messages however the archive changes meanwhile.
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
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.1 AS model, m.7 AS mode, count(), sum(m.2), sum(m.3), sum(m.4), sum(m.5), sum(m.6), min(at), max(at)
		FROM (`+messages+`)
		GROUP BY model, mode`, filter.HostID, filter.SessionID, filter.HostID, filter.HostID, filter.SessionID, filter.HostID,
		filter.HostID, filter.SessionID)
	if err != nil {
		return api.AgentUsageTotals{}, fmt.Errorf("clickhouse agent usage: %w", err)
	}
	groups, _, err := scanActivity(rows, math.MaxInt, func(g *UsageGroup) []any {
		return []any{&g.Model, &g.Mode, &g.Messages, &g.Input, &g.Output, &g.CacheCreation, &g.CacheRead, &g.Thinking, &g.FirstNs,
			&g.LastNs}
	})
	if err != nil {
		return api.AgentUsageTotals{}, err
	}
	return FoldUsage(groups), nil
}

// UsageGroup is a session's messages of one model written in one permission mode: their count, tokens, and first and last time.
type UsageGroup struct {
	api.ModelUsage
	Mode            string
	FirstNs, LastNs int64
}

// FoldUsage folds a session's message groups into its totals by model, heaviest output first, and its messages by mode, first seen
// first. Exported for the in-memory archive, so the two fold one way.
func FoldUsage(groups []UsageGroup) api.AgentUsageTotals {
	models := map[string]*api.ModelUsage{}
	modes := map[string]*api.ModeUsage{}
	for _, g := range groups {
		mu, ok := models[g.Model]
		if !ok {
			mu = &api.ModelUsage{Model: g.Model}
			models[g.Model] = mu
		}
		mu.Messages += g.Messages
		mu.Input += g.Input
		mu.Output += g.Output
		mu.CacheCreation += g.CacheCreation
		mu.CacheRead += g.CacheRead
		mu.Thinking += g.Thinking
		md, ok := modes[g.Mode]
		if !ok {
			md = &api.ModeUsage{Mode: g.Mode, FirstNs: g.FirstNs, LastNs: g.LastNs}
			modes[g.Mode] = md
		}
		md.Messages += g.Messages
		md.FirstNs, md.LastNs = min(md.FirstNs, g.FirstNs), max(md.LastNs, g.LastNs)
	}
	out := api.AgentUsageTotals{Models: []api.ModelUsage{}, Modes: []api.ModeUsage{}}
	for _, mu := range models {
		out.Models = append(out.Models, *mu)
	}
	for _, md := range modes {
		out.Modes = append(out.Modes, *md)
	}
	slices.SortFunc(out.Models, func(a, b api.ModelUsage) int {
		return cmp.Or(cmp.Compare(b.Output, a.Output), cmp.Compare(a.Model, b.Model))
	})
	slices.SortFunc(out.Modes, func(a, b api.ModeUsage) int {
		return cmp.Or(cmp.Compare(a.FirstNs, b.FirstNs), cmp.Compare(a.Mode, b.Mode))
	})
	return out
}
