package graph

import (
	"cmp"
	"context"
	"slices"

	"github.com/fleetdm/edr/server/detection/api"
)

// Service links (issue #1340). An XPC service launchd starts for an app names the app, by pid and generation, as its responsible
// process, though launchd is its parent and it carries none of the app's session markers. A responsible process is responsible for
// itself, so only such a process can be named; and a process started from a shell names the terminal or SSH login that started its
// session, not the shell, so what an agent session gains this way is the services of the apps it opened (measured on edr-dev).
const (
	// maxServiceTargets bounds how many apps one read looks for services of, the oldest kept.
	maxServiceTargets = 50
	// maxServicesPerTarget bounds the services read for each app; past it the read reports truncation.
	maxServicesPerTarget = 200
)

// serviceLinks links each process the forest holds that is responsible for itself to the launchd children that name it as
// responsible. When extend is set (a story or session read), a service the forest does not hold is returned with its descendants
// as an extra root and truncated reports a cut; a window read links only what it already holds.
func (q *Query) serviceLinks(
	ctx context.Context, hostID string, forest []api.ProcessNode, extend bool,
) (links []api.ProcessLink, extra []api.ProcessNode, truncated bool, err error) {
	held, targets := collectResponsible(forest)
	if len(targets) == 0 {
		return nil, nil, false, nil
	}
	services, err := q.store.GetResponsibleChildren(ctx, hostID, targets, maxServicesPerTarget+1)
	if err != nil {
		return nil, nil, false, err
	}
	perTarget := map[[2]int64]int{}
	var added []api.Process
	for _, s := range services {
		key := [2]int64{int64(*s.ResponsiblePID), int64(*s.ResponsiblePIDVersion)}
		perTarget[key]++
		if perTarget[key] > maxServicesPerTarget {
			truncated = true
			continue
		}
		if !held[s.ID] && !extend {
			continue
		}
		links = append(links, api.ProcessLink{From: responsibleRow(targets, key), To: s.ID, Kind: api.ProcessLinkResponsibleProcess})
		if !held[s.ID] {
			added = append(added, s)
		}
	}
	if len(added) == 0 {
		return links, nil, truncated, nil
	}
	extra, cut, err := q.launchedSubtrees(ctx, hostID, added, held)
	return links, extra, truncated || cut, err
}

// collectResponsible walks the forest for the rows it holds and those responsible for themselves, oldest first and capped.
func collectResponsible(forest []api.ProcessNode) (map[int64]bool, []api.Process) {
	held := map[int64]bool{}
	targets := []api.Process{}
	var walk func(nodes []api.ProcessNode)
	walk = func(nodes []api.ProcessNode) {
		for _, n := range nodes {
			held[n.ID] = true
			if responsibleForItself(n.Process) {
				targets = append(targets, n.Process)
			}
			walk(n.Children)
		}
	}
	walk(forest)
	slices.SortFunc(targets, func(a, b api.Process) int {
		return cmp.Or(cmp.Compare(a.ForkTimeNs, b.ForkTimeNs), cmp.Compare(a.ID, b.ID))
	})
	return held, targets[:min(len(targets), maxServiceTargets)]
}

// responsibleForItself reports a process whose exec named its own pid and generation as responsible: an app launchd started.
func responsibleForItself(p api.Process) bool {
	return p.ResponsiblePID != nil && *p.ResponsiblePID == p.PID && p.ResponsiblePIDVersion != nil && p.PIDVersion != nil &&
		*p.ResponsiblePIDVersion == *p.PIDVersion
}

// responsibleRow is the row id of the target a service names.
func responsibleRow(targets []api.Process, key [2]int64) int64 {
	i := slices.IndexFunc(targets, func(t api.Process) bool { return int64(t.PID) == key[0] && int64(*t.PIDVersion) == key[1] })
	return targets[i].ID
}
