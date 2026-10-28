package operator

import (
	"context"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/internal/graph"
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// AgentSessionTotalsReader is the read of a session's totals (issue #1422): its processes and alerts from the process store, and what
// it touched and the tools it called from the event archive. mysql.Store satisfies it.
type AgentSessionTotalsReader interface {
	AgentSessionCounts(ctx context.Context, hostID, sessionID string) (api.AgentSessionCounts, error)
	ActivityTotalsForGenerations(ctx context.Context, filter visibilityapi.ActivityFilter) (visibilityapi.ActivitySummary, error)
	AgentToolCallCounts(ctx context.Context, filter visibilityapi.AgentToolCallFilter) ([]visibilityapi.ToolCallCount, error)
}

// SetAgentSessionTotals installs the session totals read. Unset, the summary route answers 503.
func (h *Handler) SetAgentSessionTotals(r AgentSessionTotalsReader) { h.sessionTotals = r }

// agentSessionSummaryResponse is a session's totals. They are counted by the server over the whole session, so they do not depend
// on how much of it a page lists. Activity covers what the session's activity rollup covers, and Scope reports how far that reached.
type agentSessionSummaryResponse struct {
	// Commands counts the session's commands as its commands list does, over the same process read.
	Commands  int                           `json:"commands"`
	Processes int64                         `json:"processes"`
	Alerts    alertTotals                   `json:"alerts"`
	ToolCalls toolCallTotals                `json:"tool_calls"`
	Activity  visibilityapi.ActivitySummary `json:"activity"`
	Scope     timelineScope                 `json:"scope"`
}

// alertTotals counts alerts, not monitor records, in all and by severity.
type alertTotals struct {
	Total      int64            `json:"total"`
	BySeverity map[string]int64 `json:"by_severity"`
}

// toolCallTotals counts the tool calls the session recorded, in all and by tool, most used first.
type toolCallTotals struct {
	Total  int64                         `json:"total"`
	ByTool []visibilityapi.ToolCallCount `json:"by_tool"`
}

// sessionSummary counts a session's totals. One read of the session's processes gives its commands and the scope its activity is
// counted over, the scope the session's activity rollup reads, so the two agree.
func (h *Handler) sessionSummary(ctx context.Context, session *api.AgentSessionSummary) (agentSessionSummaryResponse, error) {
	hostID, sessionID := session.HostID, session.AgentSessionID
	counts, err := h.sessionTotals.AgentSessionCounts(ctx, hostID, sessionID)
	if err != nil {
		return agentSessionSummaryResponse{}, err
	}
	tree, err := h.readSession(ctx, session)
	if err != nil {
		return agentSessionSummaryResponse{}, err
	}
	_, commands := commandsOf(tree)
	roots, agents, err := h.withSessionAgents(ctx, hostID, sessionID, tree)
	if err != nil {
		return agentSessionSummaryResponse{}, err
	}
	filter, scope, _, err := h.filterOf(ctx, hostID, sessionID, roots, agents, tree.Truncated, graph.MaxScopedRead)
	if err != nil {
		return agentSessionSummaryResponse{}, err
	}
	activity, err := h.sessionTotals.ActivityTotalsForGenerations(ctx, filter)
	if err != nil {
		return agentSessionSummaryResponse{}, err
	}
	tools, err := h.sessionTotals.AgentToolCallCounts(ctx, sessionToolCalls(session))
	if err != nil {
		return agentSessionSummaryResponse{}, err
	}
	out := agentSessionSummaryResponse{
		Commands:  len(commands),
		Processes: counts.Processes,
		Alerts:    alertTotals{BySeverity: counts.AlertsBySeverity},
		ToolCalls: toolCallTotals{ByTool: tools},
		Activity:  activity,
		Scope:     *scope,
	}
	for _, n := range counts.AlertsBySeverity {
		out.Alerts.Total += n
	}
	for _, c := range tools {
		out.ToolCalls.Total += c.Count
	}
	return out, nil
}
