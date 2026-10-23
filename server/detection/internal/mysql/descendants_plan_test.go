package mysql_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/internal/mysql"
)

// TestDescendantWalk_FollowsEachEdgeByItsIndex pins the plan of the descendant walk that the agent session tree, the session's alert
// list and process search's descendant filter share. A plan is the only place this shows: the walk returns the same rows whether it
// reads an index or the whole table, and only the cost differs.
//
// The walk once joined both edges in one recursive member on their OR, which no index can serve, so every level scanned the host's
// whole table. On a dogfood host carrying 5.1M process rows the agent session tree ran past the 30-second request deadline and the
// page showed a 502. Each edge now has its own member, and each member must read processes by that edge's index.
func TestDescendantWalk_FollowsEachEdgeByItsIndex(t *testing.T) {
	t.Parallel()
	_, db := newStoreAndDB(t)
	ctx := t.Context()
	const host = "descendant-plan-host"
	// Enough rows that a full scan is not the cheapest read, so the plan reflects what MySQL does on a real table.
	seedProcesses(t, ctx, db, host, 5000, 1_000_000)
	var ancestor int64
	require.NoError(t, db.GetContext(ctx, &ancestor, "SELECT MIN(id) FROM processes WHERE host_id = ?", host))
	// Refresh the statistics after the bulk seed, which the optimizer otherwise reads as an empty table.
	_, err := db.ExecContext(ctx, "ANALYZE TABLE processes")
	require.NoError(t, err)

	rows, err := db.QueryxContext(ctx, "EXPLAIN SELECT id FROM processes WHERE host_id = ? AND "+mysql.DescendantsSQL,
		append([]any{host}, mysql.DescendantsArgs(ancestor)...)...)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	keysByMember := map[string]bool{}
	for rows.Next() {
		row := map[string]any{}
		require.NoError(t, rows.MapScan(row))
		if planCell(row["table"]) != "p" {
			continue
		}
		assert.NotEqual(t, "ALL", planCell(row["type"]), "a recursive step must not scan processes, plan row: %v", row)
		keysByMember[planCell(row["key"])] = true
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, map[string]bool{"idx_processes_host_parent": true, "idx_processes_previous_exec": true}, keysByMember,
		"the creator edge reads idx_processes_host_parent and the re-exec edge reads idx_processes_previous_exec")
}

// planCell renders a MySQL EXPLAIN cell, which arrives as []byte or nil.
func planCell(v any) string {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
