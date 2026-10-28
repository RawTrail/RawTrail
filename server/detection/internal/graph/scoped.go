package graph

import (
	"context"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

// BuildScopedTree returns one story or AI agent session as a forest (issue #1282): every row stamped with the scope, read by its
// index in one query, oldest first, rather than walked level by level from a process. A process whose creator is outside the scope
// is a root, so a session reads as its tool calls with their descendants beneath them.
//
// Truncation works as BuildTree's does: one row past the limit proves the page was cut short, and only then does the bounded count
// run. The page keeps the oldest rows, so each process's creator is on it ahead of the process. pinnedID only keeps that process
// out of a sibling aggregate; a scope read adds nothing outside the scope.
func (q *Query) BuildScopedTree(
	ctx context.Context, hostID string, scope api.TreeScope, limit int, flatten bool, pinnedID int64,
) (api.ProcessTreeResult, error) {
	// Clamped as BuildTree clamps it, for the same reasons: the arithmetic below, and a caller that did not normalize.
	limit = min(max(limit, 1), MaxScopedRead)
	procs, err := q.scopedProcesses(ctx, hostID, scope, limit+1)
	if err != nil {
		return api.ProcessTreeResult{}, err
	}
	truncated := len(procs) > limit
	if truncated {
		procs = procs[:limit]
	}
	res := api.ProcessTreeResult{Returned: int64(len(procs)), TotalMatched: int64(len(procs)), Truncated: truncated}
	// A page after the first counts what the whole scope holds, as the first does, so a client reads one total across its pages.
	if truncated || scope.After != (api.TreePosition{}) {
		total, capped, err := q.store.CountScopedProcesses(ctx, hostID, scope)
		if err != nil {
			return api.ProcessTreeResult{}, err
		}
		if truncated {
			res.TotalMatched, res.TotalMatchedCapped = resolveTotalMatched(res.Returned, total, capped)
		} else {
			// The last page proved no row beyond it; the count stands as counted, at least what this page returned.
			res.TotalMatched, res.TotalMatchedCapped = max(total, res.Returned), capped
		}
	}
	if truncated {
		last := procs[len(procs)-1]
		res.Next = &api.TreePosition{ForkTimeNs: last.ForkTimeNs, ID: last.ID}
	}
	forest := buildForest(procs, nil)
	// A story or session shows what its launchers caused launchd to run, as extra roots the links name (issue #1286).
	links, launched, launchedCut, err := q.launchLinks(ctx, hostID, forest, true)
	if err != nil {
		return api.ProcessTreeResult{}, err
	}
	forest = append(forest, inOtherScope(launched, scope)...)
	// So does what those apps asked launchd for: the services that name one of them as responsible (issue #1340), the launched apps
	// included, since an app the agent opened is the usual one.
	services, servicesExtra, servicesCut, err := q.serviceLinks(ctx, hostID, forest, true)
	if err != nil {
		return api.ProcessTreeResult{}, err
	}
	forest = append(forest, inOtherScope(servicesExtra, scope)...)
	links = append(links, services...)
	res.Links = links
	// A launched program's descendants cut at their bound are a partial read like any other.
	res.Truncated = res.Truncated || launchedCut || servicesCut
	if flatten {
		res.Roots = forest
		return res, nil
	}
	res.Roots = aggregateSiblingsKeeping(forest, linkEnds(res.Links, pinnedID))
	return res, nil
}

// inOtherScope keeps what a scoped read adds through its launches and services that is outside its scope. A process of the scope is
// read by the scope's page that holds it, so adding it through a link as well, or under a launched program that is not the scope's,
// would list it twice across the pages (issue #1424): its link still names it, and its creator id joins it to the program once its
// page is read.
func inOtherScope(extra []api.ProcessNode, scope api.TreeScope) []api.ProcessNode {
	var out []api.ProcessNode
	for _, n := range extra {
		ownSession := scope.AgentSessionID != "" && n.AgentSessionID != nil && *n.AgentSessionID == scope.AgentSessionID
		ownStory := scope.AgentSessionID == "" && n.StoryRootID != nil && *n.StoryRootID == scope.StoryRootID
		if ownSession || ownStory {
			continue
		}
		n.Children = inOtherScope(n.Children, scope)
		out = append(out, n)
	}
	return out
}

// MaxScopedRead is the most rows a scoped read lists (issue #1424): four times the tree endpoint's page, so the server shapes a
// session of up to twenty thousand processes into its commands, totals and activity whole, while a client still pages the tree.
const MaxScopedRead = 20000

// scopedChunk is how many rows one query of a scoped read fetches. A read past it continues from its last row.
const scopedChunk = 5000

// scopedProcesses reads up to limit rows of a scope from its position, oldest first, a chunk at a time, so no one query holds the
// whole read.
func (q *Query) scopedProcesses(ctx context.Context, hostID string, scope api.TreeScope, limit int) ([]api.Process, error) {
	var procs []api.Process
	for len(procs) < limit {
		chunk, err := q.store.GetScopedProcesses(ctx, hostID, scope, min(scopedChunk, limit-len(procs)))
		if err != nil {
			return nil, err
		}
		procs = append(procs, chunk...)
		if len(chunk) < scopedChunk {
			break
		}
		last := chunk[len(chunk)-1]
		scope.After = api.TreePosition{ForkTimeNs: last.ForkTimeNs, ID: last.ID}
	}
	return procs, nil
}

// maxAgentOwnProcesses bounds an agent's own processes one read lists (issue #1361). An agent runs a few for itself around each prompt;
// thousands are a long life, whose oldest are listed and the read marked as cut.
const maxAgentOwnProcesses = 2000

// BuildAgentOwnTree returns an AI agent process's own descendants forked from fromNs up to toNs (issue #1361): what it ran for
// itself, its plugin scans, shell snapshots and fork daemon among them, with none of a session's processes. They are a forest whose
// roots the agent created, nested by creator as any tree is, and the read reports whether the bound cut it.
func (q *Query) BuildAgentOwnTree(ctx context.Context, hostID string, agentID, fromNs, toNs int64) ([]api.ProcessNode, bool, error) {
	procs, err := q.store.GetAgentOwnProcesses(ctx, hostID, agentID, fromNs, toNs, maxAgentOwnProcesses+1)
	if err != nil {
		return nil, false, err
	}
	truncated := len(procs) > maxAgentOwnProcesses
	if truncated {
		procs = procs[:maxAgentOwnProcesses]
	}
	return buildForest(procs, nil), truncated, nil
}
