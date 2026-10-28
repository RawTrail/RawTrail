//go:build integration

package tests

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/internal/graph"
)

// Following a session tree's cursors to the end reads every process of the session once, in (fork time, row id) order, whatever the
// page size and however many processes share a fork time, including fork times at or before zero (issue #1447). The expected order
// is sorted here from the rows the store holds, not taken from the read under test.
//
// spec:server-rest-api/agent-session-tree-pages/a-session-tree-pages-by-cursor
func TestBuildScopedTree_CursorsReadEveryProcessOnceInOrder(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	query := graph.NewQuery(store)
	iter := 0
	rapid.Check(t, func(rt *rapid.T) {
		iter++
		host := "keyset-pbt-" + strconv.Itoa(iter)
		n := rapid.IntRange(1, 40).Draw(rt, "n")
		// A narrow range of fork times makes ties common, so page boundaries fall inside a shared time.
		forks := rapid.SliceOfN(rapid.Int64Range(-3, 3), n, n).Draw(rt, "forks")
		pageSize := rapid.IntRange(1, 7).Draw(rt, "pageSize")
		rows := make([]string, n)
		args := make([]any, 0, 3*n)
		for i, fork := range forks {
			rows[i] = "(?, ?, 1, '/bin/zsh', ?, 's')"
			args = append(args, host, 20_000+i, fork)
		}
		// One session on another host shares the session id, so a read that dropped the host would list its rows too.
		_, err := db.ExecContext(t.Context(), `INSERT INTO processes (host_id, pid, ppid, path, fork_time_ns, agent_session_id) VALUES `+
			strings.Join(rows, ", ")+", ('keyset-pbt-other', 1, 1, '/bin/zsh', 0, 's')", args...)
		require.NoError(rt, err)

		var held []api.TreePosition
		require.NoError(rt, db.SelectContext(t.Context(), &held,
			"SELECT fork_time_ns AS forktimens, id FROM processes WHERE host_id = ? AND agent_session_id = 's'", host))
		slices.SortFunc(held, func(a, b api.TreePosition) int {
			return cmp.Or(cmp.Compare(a.ForkTimeNs, b.ForkTimeNs), cmp.Compare(a.ID, b.ID))
		})

		var read []api.TreePosition
		scope := api.TreeScope{AgentSessionID: "s"}
		for page := 0; ; page++ {
			require.LessOrEqual(rt, page, n, "the cursors end")
			res, err := query.BuildScopedTree(t.Context(), host, scope, pageSize, true, 0)
			require.NoError(rt, err)
			require.LessOrEqual(rt, len(res.Roots), pageSize)
			for _, root := range res.Roots {
				read = append(read, api.TreePosition{ForkTimeNs: root.ForkTimeNs, ID: root.ID})
			}
			if res.Next == nil {
				break
			}
			scope.After = *res.Next
		}
		require.Equal(rt, held, read)
	})
}
