package sessioncommands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

// preorder lists a forest's rows in its order, each with the row of its parent in the forest (0 for a root).
func preorder(roots []api.ProcessNode) (ids []int64, parent map[int64]int64) {
	parent = map[int64]int64{}
	var visit func(p api.ProcessNode, up int64)
	visit = func(p api.ProcessNode, up int64) {
		ids = append(ids, p.ID)
		parent[p.ID] = up
		for _, c := range p.Children {
			visit(c, p.ID)
		}
	}
	for _, r := range roots {
		visit(r, 0)
	}
	return ids, parent
}

// commandOf is a command holding roots, with every process's row in ProcessIDs as Build lists them.
func commandOf(roots []api.ProcessNode) Command {
	ids, _ := preorder(roots)
	return Command{Key: "row:1", RootID: roots[0].ID, ProcessIDs: ids, Roots: roots}
}

// spec:server-rest-api/session-commands-list-their-processes-in-part/a-large-command-lists-its-first-processes-and-its-alerted-ones
func TestCap_ListsTheFirstProcessesAndTheKeptOnesUnderTheirParents(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		// A random forest: each process after the first of a root hangs under an earlier one of the same root.
		n := rapid.IntRange(1, 60).Draw(rt, "processes")
		nodes := make([]api.ProcessNode, n+1)
		under := map[int64][]int64{}
		var rootIDs []int64
		for id := int64(1); id <= int64(n); id++ {
			nodes[id] = api.ProcessNode{Process: api.Process{ID: id}}
			if id == 1 || rapid.IntRange(0, 9).Draw(rt, "new root") == 0 {
				rootIDs = append(rootIDs, id)
				continue
			}
			up := rapid.Int64Range(1, id-1).Draw(rt, "parent")
			under[up] = append(under[up], id)
		}
		var build func(id int64) api.ProcessNode
		build = func(id int64) api.ProcessNode {
			p := nodes[id]
			for _, c := range under[id] {
				p.Children = append(p.Children, build(c))
			}
			return p
		}
		var roots []api.ProcessNode
		for _, r := range rootIDs {
			roots = append(roots, build(r))
		}
		c := commandOf(roots)
		inOrder, parent := preorder(roots)
		limit := rapid.IntRange(1, n+5).Draw(rt, "limit")
		kept := map[int64]bool{}
		for _, id := range rapid.SliceOfDistinct(rapid.Int64Range(1, int64(n)), func(id int64) int64 { return id }).Draw(rt, "kept") {
			kept[id] = true
		}

		got := Cap(c, limit, func(p api.ProcessNode) bool { return kept[p.ID] })
		require.Equal(rt, c.ProcessIDs, got.ProcessIDs, "the command's rows stay whole")
		if n <= limit {
			require.Equal(rt, c, got, "a command within the cap is listed whole")
			return
		}

		// The rows that must be listed: the first limit in order, the kept ones, and every row above either.
		want := map[int64]bool{}
		for i, id := range inOrder {
			if i < limit || kept[id] {
				for at := id; at != 0 && !want[at]; at = parent[at] {
					want[at] = true
				}
			}
		}
		listed, listedParent := preorder(got.Roots)
		var wantInOrder []int64
		for _, id := range inOrder {
			if want[id] {
				wantInOrder = append(wantInOrder, id)
			}
		}
		require.Equal(rt, wantInOrder, listed, "exactly those rows, in the forest's order")
		require.Equal(rt, len(listed) < n, got.ProcessesCut, "marked cut exactly when a process is left out")
		for _, id := range listed {
			require.Equal(rt, parent[id], listedParent[id], "row %d stays under its parent", id)
		}
	})
}

func TestCap_AnEmptyQueryKeepsNothing(t *testing.T) {
	t.Parallel()
	c := Command{FromAgent: true, RootID: 1}
	wrapper := api.ProcessNode{Process: api.Process{ID: 1, Path: "/bin/zsh"}}
	tool := api.ProcessNode{Process: api.Process{ID: 2, Path: "/usr/bin/make"}}
	assert.False(t, Matching(c, "  ")(tool))
	assert.True(t, Matching(c, " MAKE ")(tool), "the query is matched as Search matches it")
	assert.True(t, Matching(c, "zsh")(wrapper), "the wrapper's path is searched")
}
