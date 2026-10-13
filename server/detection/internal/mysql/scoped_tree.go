package mysql

import (
	"context"
	"fmt"

	"github.com/fleetdm/edr/server/detection/api"
)

// scopePredicate is the indexed equality a scoped tree read filters by, after host_id: the agent session served by
// idx_processes_host_agent_session, or the story served by idx_processes_host_story_root (migration 00019).
func scopePredicate(scope api.TreeScope) (string, any) {
	if scope.AgentSessionID != "" {
		return "agent_session_id = ?", scope.AgentSessionID
	}
	return "story_root_id = ?", scope.StoryRootID
}

// GetScopedProcesses returns up to limit rows of one story or agent session on the host (issue #1282), oldest first, so a page the
// limit cut short keeps each process's creator ahead of it. One indexed read, whatever the depth of the tree.
func (s *Store) GetScopedProcesses(ctx context.Context, hostID string, scope api.TreeScope, limit int) ([]api.Process, error) {
	predicate, value := scopePredicate(scope)
	procs := make([]api.Process, 0, limit)
	if err := s.db.SelectContext(ctx, &procs, `
		SELECT `+processReadColumns+`
		FROM processes
		WHERE host_id = ? AND `+predicate+`
		ORDER BY fork_time_ns, id
		LIMIT ?`,
		hostID, value, limit,
	); err != nil {
		return nil, fmt.Errorf("query scoped processes: %w", err)
	}
	return procs, nil
}

// CountScopedProcesses counts one story's or agent session's rows on the host, at most ProcessTreeCountBound, with the same capped
// meaning as CountProcessTree. The index range bounds the work, so unlike the window count it needs no time budget.
func (s *Store) CountScopedProcesses(ctx context.Context, hostID string, scope api.TreeScope) (int64, bool, error) {
	predicate, value := scopePredicate(scope)
	var total int64
	if err := s.db.GetContext(ctx, &total, `
		SELECT COUNT(*) FROM (
			SELECT 1 FROM processes WHERE host_id = ? AND `+predicate+` LIMIT ?
		) AS bounded`,
		hostID, value, ProcessTreeCountBound+1,
	); err != nil {
		return 0, false, fmt.Errorf("count scoped processes: %w", err)
	}
	if total > ProcessTreeCountBound {
		return ProcessTreeCountBound, true, nil
	}
	return total, false, nil
}
