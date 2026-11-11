package operator

import (
	"context"

	"golang.org/x/sync/errgroup"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/internal/graph"
	"github.com/rawtrail/rawtrail/server/detection/internal/sessioncommands"
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// AgentSessionTotalsReader is the read of a session's totals (issue #1422): its processes and alerts from the process store, and the
// tools it called, the tokens it used (issue #1427) and its digest (issue #1429), which totals what it touched, from the event archive.
// mysql.Store satisfies it.
type AgentSessionTotalsReader interface {
	AgentSessionCounts(ctx context.Context, hostID, sessionID string) (api.AgentSessionCounts, error)
	AgentToolCallCounts(ctx context.Context, filter visibilityapi.AgentToolCallFilter) ([]visibilityapi.ToolCallCount, error)
	AgentUsageTotals(ctx context.Context, filter visibilityapi.AgentToolCallFilter, ownerUID int64) (visibilityapi.AgentUsageTotals, error)
	ActivityDigest(ctx context.Context, filter visibilityapi.DigestFilter) (visibilityapi.ActivityDigest, error)
	AgentPromptCount(ctx context.Context, hostID, sessionID string) (int64, error)
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
	// Usage is the tokens the session's messages used, by model, and its messages by permission mode (issue #1427).
	Usage visibilityapi.AgentUsageTotals `json:"usage"`
	// Digest describes the session for its Summary (issue #1429), over the same scope as Activity.
	Digest sessionDigest `json:"digest"`
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
// counted over, the scope the session's activity rollup reads, so the two agree. The reads that need neither run beside it, so a long
// session's Summary waits for its slowest read rather than for all of them in turn (issue #1550).
func (h *Handler) sessionSummary(ctx context.Context, session *api.AgentSessionSummary) (agentSessionSummaryResponse, error) {
	hostID, sessionID := session.HostID, session.AgentSessionID
	var (
		counts  api.AgentSessionCounts
		tools   []visibilityapi.ToolCallCount
		prompts int64
		scoped  scopedSummary
		// Usage counts only from the owner's transcripts (issue #1478), so a session whose owner is no longer known reports none, as
		// its lineage does.
		usage = visibilityapi.AgentUsageTotals{Models: []visibilityapi.ModelUsage{}, Modes: []visibilityapi.ModeUsage{}}
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		counts, err = h.sessionTotals.AgentSessionCounts(gctx, hostID, sessionID)
		return err
	})
	g.Go(func() (err error) {
		tools, err = h.sessionTotals.AgentToolCallCounts(gctx, sessionToolCalls(session))
		return err
	})
	if session.OwnerUID != nil {
		g.Go(func() (err error) {
			usage, err = h.sessionTotals.AgentUsageTotals(gctx, sessionToolCalls(session), *session.OwnerUID)
			return err
		})
	}
	g.Go(func() (err error) {
		prompts, err = h.sessionTotals.AgentPromptCount(gctx, hostID, sessionID)
		return err
	})
	g.Go(func() (err error) {
		scoped, err = h.scopedSummary(gctx, session)
		return err
	})
	if err := g.Wait(); err != nil {
		return agentSessionSummaryResponse{}, err
	}
	out := agentSessionSummaryResponse{
		Commands:  len(scoped.commands),
		Processes: counts.Processes,
		Alerts:    alertTotals{BySeverity: counts.AlertsBySeverity},
		ToolCalls: toolCallTotals{ByTool: tools},
		Activity:  scoped.digest.Totals,
		Scope:     scoped.scope,
		Usage:     usage,
		Digest: sessionDigest{
			Prompts:         prompts,
			Programs:        topPrograms(scoped.commands),
			ActivityDigest:  scoped.digest,
			CommandTimeline: commandTimeline(scoped.commands, scoped.digest),
		},
	}
	for _, n := range counts.AlertsBySeverity {
		out.Alerts.Total += n
	}
	for _, c := range tools {
		out.ToolCalls.Total += c.Count
	}
	return out, nil
}

// scopedSummary is what a Summary reads through the session's processes: its commands, the scope its activity is counted over, and
// the digest of that scope, which totals it.
type scopedSummary struct {
	commands []sessioncommands.Command
	scope    timelineScope
	digest   visibilityapi.ActivityDigest
}

// scopedSummary reads the session's processes, shapes them into its commands and its activity scope, and reads the scope's digest.
func (h *Handler) scopedSummary(ctx context.Context, session *api.AgentSessionSummary) (scopedSummary, error) {
	tree, err := h.readSession(ctx, session)
	if err != nil {
		return scopedSummary{}, err
	}
	_, commands := commandsOf(tree)
	roots, agents, err := h.withSessionAgents(ctx, session.HostID, session.AgentSessionID, tree)
	if err != nil {
		return scopedSummary{}, err
	}
	filter, scope, _, err := h.filterOf(ctx, session.HostID, session.AgentSessionID, roots, agents, tree.Truncated, graph.MaxScopedRead)
	if err != nil {
		return scopedSummary{}, err
	}
	digest, err := h.sessionTotals.ActivityDigest(ctx, digestFilter(session, filter, commands))
	if err != nil {
		return scopedSummary{}, err
	}
	return scopedSummary{commands: commands, scope: *scope, digest: digest}, nil
}
