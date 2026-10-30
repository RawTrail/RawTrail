package sessioncommands

import (
	"cmp"
	"slices"
	"strings"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

// SearchProcesses lists every process of a session's forest whose path or command line holds the query, regardless of case, each
// on its own and in fork order (issue #1452). The Processes view lists what a search finds this way, since the tree a match sits in
// may lie on a page not yet read. An empty query lists every process.
func SearchProcesses(forest []api.ProcessNode, rawQuery string) []api.ProcessNode {
	query := strings.ToLower(strings.TrimSpace(rawQuery))
	found := []api.ProcessNode{}
	for _, root := range forest {
		walk(root, func(p api.ProcessNode) {
			if matchesProcess(p, query, false) {
				p.Children = nil
				found = append(found, p)
			}
		})
	}
	slices.SortFunc(found, func(a, b api.ProcessNode) int {
		return cmp.Or(cmp.Compare(a.ForkTimeNs, b.ForkTimeNs), cmp.Compare(a.ID, b.ID))
	})
	return found
}

// ProcessPageOf returns the limit processes after a position in fork order, the first limit for the zero position, and the position
// of the page's last process when more follow.
func ProcessPageOf(procs []api.ProcessNode, after api.TreePosition, limit int) ([]api.ProcessNode, *api.TreePosition) {
	limit = max(limit, 1)
	from := 0
	if after != (api.TreePosition{}) {
		from, _ = slices.BinarySearchFunc(procs, after, func(p api.ProcessNode, at api.TreePosition) int {
			return cmp.Or(cmp.Compare(p.ForkTimeNs, at.ForkTimeNs), cmp.Compare(p.ID, at.ID))
		})
		if from < len(procs) && procs[from].ForkTimeNs == after.ForkTimeNs && procs[from].ID == after.ID {
			from++
		}
	}
	page := procs[from:min(from+limit, len(procs))]
	if from+limit >= len(procs) {
		return page, nil
	}
	last := page[len(page)-1]
	return page, &api.TreePosition{ForkTimeNs: last.ForkTimeNs, ID: last.ID}
}
