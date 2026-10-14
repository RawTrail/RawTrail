package mysql

import (
	"context"
	"fmt"

	"github.com/fleetdm/edr/server/detection/api"
)

// GetLaunchdChildren returns the host's processes launchd forked (ppid 1) with a fork time inside [fromNs, toNs], oldest first and at
// most limit: the candidates an inferred launch is matched among (issue #1286). A launchd-started program is an xpcproxy row and the
// re-exec of the same pid into the program, so both appear. The (host_id, ppid, fork_time_ns) index serves the range.
func (s *Store) GetLaunchdChildren(ctx context.Context, hostID string, fromNs, toNs int64, limit int) ([]api.Process, error) {
	procs := []api.Process{}
	if err := s.db.SelectContext(ctx, &procs, `
		SELECT `+processReadColumns+`
		FROM processes
		WHERE host_id = ? AND ppid = 1 AND fork_time_ns BETWEEN ? AND ?
		ORDER BY fork_time_ns, id
		LIMIT ?`,
		hostID, fromNs, toNs, limit,
	); err != nil {
		return nil, fmt.Errorf("query launchd children: %w", err)
	}
	return procs, nil
}
