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
// responsible within its lifetime. A story or session read (extend) reads them, adding each service the forest does not hold with its
// descendants as an extra root, and reports a cut: more apps than are looked at, or more services for one app than are read. A
// window read holds both ends of every link it can draw, so it matches what it holds and reads nothing.
func (q *Query) serviceLinks(
	ctx context.Context, hostID string, forest []api.ProcessNode, extend bool,
) (links []api.ProcessLink, extra []api.ProcessNode, truncated bool, err error) {
	held, rows, targets := collectResponsible(forest)
	if len(targets) == 0 {
		return nil, nil, false, nil
	}
	if !extend {
		for _, r := range rows {
			if owner, ok := serviceOwner(targets, r); ok {
				links = append(links, api.ProcessLink{From: owner, To: r.ID, Kind: api.ProcessLinkResponsibleProcess})
			}
		}
		return links, nil, false, nil
	}
	truncated = len(targets) > maxServiceTargets
	targets = targets[:min(len(targets), maxServiceTargets)]
	services, err := q.store.GetResponsibleChildren(ctx, hostID, targets, maxServicesPerTarget+1)
	if err != nil {
		return nil, nil, false, err
	}
	perTarget := map[int64]int{}
	var added []api.Process
	for _, s := range services {
		owner, ok := serviceOwner(targets, s)
		if !ok {
			continue
		}
		perTarget[owner]++
		if perTarget[owner] > maxServicesPerTarget {
			truncated = true
			continue
		}
		links = append(links, api.ProcessLink{From: owner, To: s.ID, Kind: api.ProcessLinkResponsibleProcess})
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

// collectResponsible walks the forest for the rows it holds and, oldest first, those responsible for themselves.
func collectResponsible(forest []api.ProcessNode) (map[int64]bool, []api.Process, []api.Process) {
	held := map[int64]bool{}
	rows := []api.Process{}
	targets := []api.Process{}
	var walk func(nodes []api.ProcessNode)
	walk = func(nodes []api.ProcessNode) {
		for _, n := range nodes {
			held[n.ID] = true
			rows = append(rows, n.Process)
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
	return held, rows, targets
}

// responsibleForItself reports a process whose exec named its own pid and generation as responsible: an app launchd started.
func responsibleForItself(p api.Process) bool {
	return p.ResponsiblePID != nil && *p.ResponsiblePID == p.PID && p.ResponsiblePIDVersion != nil && p.PIDVersion != nil &&
		*p.ResponsiblePIDVersion == *p.PIDVersion
}

// serviceOwner returns the row id of the target a launchd child is a service of: the one it names by pid and generation whose
// lifetime holds the service's start, since a generation counter restarts with the host and a pair can recur. A process launchd
// adopted rather than started, a process naming no other, and one naming none of the targets are no service.
func serviceOwner(targets []api.Process, s api.Process) (int64, bool) {
	if s.PPID != 1 || s.Reparented || s.ResponsiblePID == nil || s.ResponsiblePIDVersion == nil || responsibleForItself(s) {
		return 0, false
	}
	for _, t := range targets {
		alive := s.ForkTimeNs >= t.ForkTimeNs && (t.ExitTimeNs == nil || s.ForkTimeNs <= *t.ExitTimeNs)
		if t.PID == *s.ResponsiblePID && *t.PIDVersion == *s.ResponsiblePIDVersion && alive {
			return t.ID, true
		}
	}
	return 0, false
}
