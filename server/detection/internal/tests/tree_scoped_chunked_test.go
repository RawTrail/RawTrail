//go:build integration

// A scoped read past one query's chunk (issue #1424): a session larger than a tree page is read whole, a chunk at a time, in fork
// order, with rows that share a fork time taken by row id across the chunk boundary.

package tests

import (
	"strconv"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/internal/graph"
)

// seedLargeSession inserts n processes of one session, three to a fork time, so chunk boundaries fall inside a shared time.
func seedLargeSession(t *testing.T, db *sqlx.DB, host, session string, n int) {
	t.Helper()
	const batch = 1000
	for start := 0; start < n; start += batch {
		var rows []string
		var args []any
		for i := start; i < min(start+batch, n); i++ {
			rows = append(rows, "(?, ?, 1, '/bin/zsh', ?, ?)")
			args = append(args, host, 10_000+i, int64(1_000+i/3), session)
		}
		_, err := db.ExecContext(t.Context(), `INSERT INTO processes (host_id, pid, ppid, path, fork_time_ns, agent_session_id) VALUES `+
			strings.Join(rows, ", "), args...)
		require.NoError(t, err)
	}
}

// spec:server-rest-api/agent-session-reads-past-one-page/a-session-past-the-tree-page-is-read-whole
func TestBuildScopedTree_ReadsPastOneChunk(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	const size = 12_346
	seedLargeSession(t, db, "host-a", "big", size)
	seedLargeSession(t, db, "host-a", "other", 10)
	query := graph.NewQuery(store)
	scope := api.TreeScope{AgentSessionID: "big"}

	whole, err := query.BuildScopedTree(t.Context(), "host-a", scope, graph.MaxScopedRead, true, 0)
	require.NoError(t, err)
	assert.False(t, whole.Truncated)
	require.Len(t, whole.Roots, size, "every process, none twice")
	pids := map[int]bool{}
	for _, root := range whole.Roots {
		require.False(t, pids[root.PID], "pid %d listed twice", root.PID)
		pids[root.PID] = true
	}
	assert.True(t, pids[10_000] && pids[10_000+size-1], "the first process and the last")

	// The store's read in chunks lists the rows in fork order, ties by row, each once, across every chunk boundary.
	var read []int
	paged := scope
	for range size {
		chunk, err := store.GetScopedProcesses(t.Context(), "host-a", paged, 1_000)
		require.NoError(t, err)
		for _, p := range chunk {
			read = append(read, p.PID)
		}
		if len(chunk) < 1_000 {
			break
		}
		paged.After = api.TreePosition{ForkTimeNs: chunk[len(chunk)-1].ForkTimeNs, ID: chunk[len(chunk)-1].ID}
	}
	require.Len(t, read, size)
	for i, pid := range read {
		require.Equal(t, 10_000+i, pid, "position "+strconv.Itoa(i))
	}

	cut, err := query.BuildScopedTree(t.Context(), "host-a", scope, 6_000, true, 0)
	require.NoError(t, err)
	assert.True(t, cut.Truncated)
	assert.Len(t, cut.Roots, 6_000)
	assert.Equal(t, int64(size), cut.TotalMatched, "the count reaches past the read")
	assert.False(t, cut.TotalMatchedCapped)
}

// A client pages a session's tree from each page's last row (issue #1424): every row is read once across the pages, and each page
// reports the session's total.
//
// spec:server-rest-api/agent-session-tree-pages/a-session-tree-pages-by-cursor
func TestBuildScopedTree_PagesFromItsLastRow(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	const size = 12_346
	seedLargeSession(t, db, "host-a", "paged", size)
	query := graph.NewQuery(store)
	scope := api.TreeScope{AgentSessionID: "paged"}
	seen := map[int64]bool{}
	for page := 0; ; page++ {
		res, err := query.BuildScopedTree(t.Context(), "host-a", scope, 5_000, true, 0)
		require.NoError(t, err)
		assert.Equal(t, int64(size), res.TotalMatched, "page %d reports the session's total", page)
		for _, root := range res.Roots {
			require.False(t, seen[root.ID], "row %d read twice", root.ID)
			seen[root.ID] = true
		}
		if res.Next == nil {
			break
		}
		require.Less(t, page, 3, "three pages hold the session")
		scope.After = *res.Next
	}
	assert.Len(t, seen, size)
}
