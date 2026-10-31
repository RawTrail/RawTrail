package clickhouse

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/rawtrail/rawtrail/server/visibility/api"
)

// usageMessageID is an agent_usage event's message id, which tells a message's entries apart as tool_use_id does a call's.
const usageMessageID = "JSONExtractString(payload, 'message_id')"

// notCopiedUsageSQL keeps a session's messages that are not copies of another session's, as notCopiedSQL does its calls.
var notCopiedUsageSQL = notCopied("agent_usage", usageMessageID)

// AgentUsageTotals totals one session's message usage by model and counts its messages by permission mode (issue #1427), each
// message once by its id and owner and the copies of another session's left out. One read groups the messages by model and mode,
// and both breakdowns are folded from it, so they describe the same messages however the archive changes meanwhile.
func (s *Store) AgentUsageTotals(ctx context.Context, filter api.AgentToolCallFilter, ownerUID int64) (api.AgentUsageTotals, error) {
	// messages is one row per message and owner, at its first recording: a re-read transcript sends the same message again.
	messages := `
		SELECT ` + usageMessageID + ` AS id, ` + callUID + ` AS uid,
			argMin((JSONExtractString(payload, 'model'), JSONExtractInt(payload, 'input_tokens'), JSONExtractInt(payload, 'output_tokens'),
				JSONExtractInt(payload, 'cache_creation_input_tokens'), JSONExtractInt(payload, 'cache_read_input_tokens'),
				JSONExtractInt(payload, 'thinking_tokens'), JSONExtractString(payload, 'permission_mode')), (timestamp_ns, event_id)) AS m,
			min(timestamp_ns) AS at
		FROM events FINAL
		WHERE ` + hostPredicate + ` AND event_type = 'agent_usage' AND ` + callSession + ` = ? AND ` + callUID + ` = ?
			AND ` + notCopiedUsageSQL + `
		GROUP BY id, uid`
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.1 AS model, m.7 AS mode, count(), sum(m.2), sum(m.3), sum(m.4), sum(m.5), sum(m.6), min(at), max(at)
		FROM (`+messages+`)
		GROUP BY model, mode`, filter.HostID, filter.SessionID, ownerUID, filter.HostID, filter.HostID, filter.SessionID, filter.HostID,
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

// AgentSessionTokens totals each listed session's tokens with one read over the listed hosts (issue #1427). A session's messages are
// counted as AgentUsageTotals counts them: from its owner's transcripts, once each by id, at the first recording, leaving out the
// copies a session they name holds, which here may be any session on the same host.
func (s *Store) AgentSessionTokens(ctx context.Context, sessions []api.AgentSessionRef) (map[api.AgentSessionRef]int64, error) {
	out := map[api.AgentSessionRef]int64{}
	if len(sessions) == 0 {
		return out, nil
	}
	hostSet := map[string]bool{}
	var hosts []any
	var pairs []any
	for _, r := range sessions {
		if !hostSet[r.HostID] {
			hostSet[r.HostID] = true
			hosts = append(hosts, r.HostID)
		}
		pairs = append(pairs, r.HostID, r.SessionID, r.OwnerUID)
	}
	hostsIn := "host_id IN (" + placeholders(len(hosts)) + ")"
	// A session's messages are those naming it from its owner's transcripts (issue #1478).
	pairsIn := "(host_id, " + callSession + ", " + callUID + ") IN (" +
		strings.TrimSuffix(strings.Repeat("(?, ?, ?), ", len(sessions)), ", ") + ")"
	usage := "events FINAL WHERE " + hostsIn + " AND event_type = 'agent_usage'"
	// held is the messages of every session a listed session's messages name as their source, the sessions a copy can come from.
	held := `SELECT host_id, ` + callSession + `, ` + usageMessageID + `, ` + callUID + ` FROM ` + usage + ` AND (host_id, ` + callSession +
		`) IN (SELECT host_id, arrayJoin([` + callOrigin + `, ` + callForked + `]) FROM ` + usage + ` AND ` + pairsIn + `)`
	query := `
		SELECT host_id, session, uid, sum(tokens) FROM (
			SELECT host_id, ` + callSession + ` AS session, ` + usageMessageID + ` AS id, ` + callUID + ` AS uid,
				argMin(JSONExtractInt(payload, 'input_tokens') + JSONExtractInt(payload, 'output_tokens') +
					JSONExtractInt(payload, 'cache_creation_input_tokens') + JSONExtractInt(payload, 'cache_read_input_tokens'),
					(timestamp_ns, event_id)) AS tokens
			FROM ` + usage + ` AND ` + pairsIn + `
				AND NOT ((` + callOrigin + ` != '' AND (host_id, ` + callOrigin + `, ` + usageMessageID + `, ` + callUID + `) IN (` + held + `))
					OR (` + callForked + ` != '' AND (host_id, ` + callForked + `, ` + usageMessageID + `, ` + callUID + `) IN (` + held + `)))
			GROUP BY host_id, session, id, uid)
		GROUP BY host_id, session, uid`
	var args []any
	args = append(args, hosts...)
	args = append(args, pairs...)
	for range 2 {
		args = append(args, hosts...)
		args = append(args, hosts...)
		args = append(args, pairs...)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse agent session tokens: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ref api.AgentSessionRef
		var tokens int64
		if err := rows.Scan(&ref.HostID, &ref.SessionID, &ref.OwnerUID, &tokens); err != nil {
			return nil, fmt.Errorf("scan agent session tokens: %w", err)
		}
		out[ref] = tokens
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent session tokens: %w", err)
	}
	return out, nil
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
