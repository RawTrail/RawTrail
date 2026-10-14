package mysql

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/jmoiron/sqlx"

	"github.com/fleetdm/edr/server/detection/api"
)

// GetLaunchdChildren returns the host's processes launchd forked with a fork time inside any of the ranges, oldest first, at most
// perRange from each range: the candidates inferred launches are matched among (issue #1286), every launcher's window in one read.
// Each range has its own bound, so a busy early window cannot use up the rows a later one needs. Overlapping ranges return a row
// once per range, which the matching tolerates: a launcher takes its first match and a row is linked once. A launchd-started program is an xpcproxy row and the re-exec of the same pid into the program, so both
// appear. A row first seen at exec of an already-daemonized process also has ppid 1 but is marked reparented: launchd adopted it
// rather than started it, so it is left out. The (host_id, ppid, fork_time_ns) index serves each range.
func (s *Store) GetLaunchdChildren(ctx context.Context, hostID string, ranges []api.TimeRange, perRange int) ([]api.Process, error) {
	if len(ranges) == 0 {
		return []api.Process{}, nil
	}
	branches := make([]string, len(ranges))
	var args []any
	for i, r := range ranges {
		branches[i] = `(SELECT ` + processReadColumns + `
			FROM processes
			WHERE host_id = ? AND ppid = 1 AND NOT COALESCE(reparented, FALSE) AND fork_time_ns BETWEEN ? AND ?
			ORDER BY fork_time_ns, id
			LIMIT ?)`
		args = append(args, hostID, r.FromNs, r.ToNs, perRange)
	}
	procs := []api.Process{}
	if err := s.db.SelectContext(ctx, &procs, strings.Join(branches, " UNION ALL "), args...); err != nil {
		return nil, fmt.Errorf("query launchd children: %w", err)
	}
	slices.SortFunc(procs, func(a, b api.Process) int {
		return cmp.Or(cmp.Compare(a.ForkTimeNs, b.ForkTimeNs), cmp.Compare(a.ID, b.ID))
	})
	return procs, nil
}

// GetStoriesProcesses returns the rows of the given stories on the host, oldest first and at most limit, for the launched programs a
// session or story read adds (issue #1286).
func (s *Store) GetStoriesProcesses(ctx context.Context, hostID string, roots []int64, limit int) ([]api.Process, error) {
	procs := []api.Process{}
	if len(roots) == 0 {
		return procs, nil
	}
	query, args, err := sqlx.In(`
		SELECT `+processReadColumns+`
		FROM processes
		WHERE host_id = ? AND story_root_id IN (?)
		ORDER BY fork_time_ns, id
		LIMIT ?`, hostID, roots, limit)
	if err != nil {
		return nil, fmt.Errorf("build stories query: %w", err)
	}
	if err := s.db.SelectContext(ctx, &procs, s.db.Rebind(query), args...); err != nil {
		return nil, fmt.Errorf("query stories processes: %w", err)
	}
	return procs, nil
}
