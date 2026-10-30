package operator

import (
	"context"
	"math"
	"net/url"
	"strconv"

	"github.com/rawtrail/rawtrail/server/detection/api"
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// timelineScope is what a scoped host-timeline read reports about its scope (issue #1282), so the client can say how much of it the
// events reflect without assembling the scope itself.
type timelineScope struct {
	// Applied is true when at least one of the scope's processes carries a generation, so the events are narrowed to the scope.
	// When false the response is the whole host's stream, and Processes says why: 0 means the scope named nothing stored.
	Applied bool `json:"applied"`
	// Processes counts the scope's processes, and Unmatched those of them with no generation, whose events cannot be selected and
	// are therefore missing from an applied scope.
	Processes int `json:"processes"`
	Unmatched int `json:"unmatched"`
	// Truncated reports that the scope had more processes than the timeline reads, so the events reflect only part of it.
	Truncated bool `json:"truncated"`
}

// hostTimelineResponse is the timeline page, with the scope report when the read was scoped.
type hostTimelineResponse struct {
	visibilityapi.EventSearchResult
	Scope *timelineScope `json:"scope,omitempty"`
}

// timelineScopeRequest is a scope named by ?scope=: an alert chain by its pinned process, or a story or session as the tree reads them.
type timelineScopeRequest struct {
	chainPin int64
	tree     api.TreeScope
}

// parseTimelineScope reads ?scope=chain&pin=<process id>, ?scope=session&session=<id> or ?scope=story&root=<process id>. scoped is
// false when no scope is named; valid is false when one is named without a usable target.
func parseTimelineScope(q url.Values) (req timelineScopeRequest, scoped, valid bool) {
	if q.Get("scope") == "chain" {
		pin, err := strconv.ParseInt(q.Get("pin"), 10, 64)
		return timelineScopeRequest{chainPin: pin}, true, err == nil && pin > 0
	}
	tree, scoped, valid := parseTreeScope(q)
	return timelineScopeRequest{tree: tree}, scoped, valid
}

// resolveTimelineScope reads the scope's processes with the same reads the process tree uses (the chain read for an alert chain, the
// indexed story or session read otherwise), and returns the generations the timeline filters on with the report of how far they
// reach. toNs bounds a chain's descendants as the timeline's window does; 0 leaves them unbounded.
func (h *Handler) resolveTimelineScope(
	ctx context.Context, hostID string, req timelineScopeRequest, toNs int64,
) ([]visibilityapi.ProcessGeneration, *timelineScope, error) {
	var res api.ProcessTreeResult
	var err error
	if req.chainPin != 0 {
		if toNs == 0 {
			toNs = math.MaxInt64
		}
		res, err = h.svc.BuildChainTree(ctx, hostID, api.TimeRange{ToNs: toNs}, req.chainPin, true)
	} else {
		res, err = h.svc.BuildScopedTree(ctx, hostID, req.tree, maxTimelineChain, true, 0)
	}
	if err != nil {
		return nil, nil, err
	}
	generations, scope, _ := scopeGenerations(res.Roots, res.Truncated, maxTimelineChain)
	return generations, scope, nil
}

// scopeGenerations walks a scope's processes and returns up to bound generations an archive read filters on, the report of how far
// they reach, and the processes that carry one. Shared by the timeline and activity scopes so they count a scope alike.
func scopeGenerations(
	roots []api.ProcessNode, truncated bool, bound int,
) ([]visibilityapi.ProcessGeneration, *timelineScope, []api.ProcessNode) {
	scope := &timelineScope{Truncated: truncated}
	var generations []visibilityapi.ProcessGeneration
	matched := []api.ProcessNode{}
	// Pre-order, so a scope cut at the filter cap keeps the roots and the pinned process ahead of the deepest descendants.
	var walk func(nodes []api.ProcessNode)
	walk = func(nodes []api.ProcessNode) {
		for _, n := range nodes {
			scope.Processes++
			if n.PIDVersion == nil {
				scope.Unmatched++
			} else {
				generations = append(generations, visibilityapi.ProcessGeneration{PID: int64(n.PID), PIDVersion: int64(*n.PIDVersion)})
				matched = append(matched, n)
			}
			walk(n.Children)
		}
	}
	walk(roots)
	if len(generations) > bound {
		generations, matched, scope.Truncated = generations[:bound], matched[:bound], true
	}
	scope.Applied = len(generations) > 0
	return generations, scope, matched
}
