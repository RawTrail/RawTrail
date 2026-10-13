package graph

import (
	"context"

	"github.com/fleetdm/edr/server/detection/api"
	"github.com/fleetdm/edr/server/detection/internal/mysql"
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
	limit = min(max(limit, 1), mysql.ProcessTreeCountBound-1)
	procs, err := q.store.GetScopedProcesses(ctx, hostID, scope, limit+1)
	if err != nil {
		return api.ProcessTreeResult{}, err
	}
	truncated := len(procs) > limit
	if truncated {
		procs = procs[:limit]
	}
	res := api.ProcessTreeResult{Returned: int64(len(procs)), TotalMatched: int64(len(procs)), Truncated: truncated}
	if truncated {
		total, capped, err := q.store.CountScopedProcesses(ctx, hostID, scope)
		if err != nil {
			return api.ProcessTreeResult{}, err
		}
		res.TotalMatched, res.TotalMatchedCapped = resolveTotalMatched(res.Returned, total, capped)
	}
	forest := buildForest(procs, nil)
	if flatten {
		res.Roots = forest
		return res, nil
	}
	res.Roots = aggregateSiblingsPinned(forest, pinnedID)
	return res, nil
}
