//go:build integration

// The tree reads link by the creator row the graph builder resolved (issue #1280): the forest, the pinned ancestor walk and the chain's
// descendant walk. Rows are seeded directly so each read is tested against links the builder would have written, including the ones
// the newest-row-per-pid and pid-at-an-instant rules get wrong.

package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fleetdm/edr/server/detection/api"
	"github.com/fleetdm/edr/server/detection/internal/graph"
	"github.com/fleetdm/edr/server/detection/internal/mysql"
)

// treeParents flattens a forest into row id -> parent row id, with 0 for a root.
func treeParents(roots []api.ProcessNode) map[int64]int64 {
	out := map[int64]int64{}
	var walk func(parent int64, nodes []api.ProcessNode)
	walk = func(parent int64, nodes []api.ProcessNode) {
		for _, n := range nodes {
			out[n.ID] = parent
			walk(n.ID, n.Children)
		}
	}
	walk(0, roots)
	return out
}

// creatorRows seeds two shapes on one host and returns the row ids by name.
//
//   - Two processes hold pid 500 at once: zsh (forked at 1000) and python3 (forked at 1600), recorded before zsh's exit arrived.
//     A child (pid 600) forked at 1650 was created by zsh, which every rule keyed on pid and time gets wrong: python3 is the newest
//     row holding 500 and the one alive with the latest fork.
//   - A double fork: bash (4600) forked a subshell (4650), which forked a daemon (4700) and exited before launchd adopted it. A later
//     process reused pid 4650.
func creatorRows(t *testing.T, store *mysql.Store, host string) map[string]int64 {
	t.Helper()
	ids := map[string]int64{}
	insert := func(name string, p api.Process, parent string) {
		p.HostID = host
		if parent != "" {
			id := ids[parent]
			p.ParentProcessID = &id
		}
		id, err := store.InsertProcess(t.Context(), p)
		require.NoError(t, err)
		ids[name] = id
	}
	exited := func(ns int64) *int64 { return &ns }
	insert("zsh", api.Process{PID: 500, PPID: 1, Path: "/bin/zsh", ForkTimeNs: 1000, PIDVersion: new(uint32(7301))}, "")
	insert("python3", api.Process{PID: 500, PPID: 1, Path: "/usr/bin/python3", ForkTimeNs: 1600, PIDVersion: new(uint32(7351))}, "")
	insert("child", api.Process{PID: 600, PPID: 500, Path: "/bin/ls", ForkTimeNs: 1650, PIDVersion: new(uint32(7360)),
		ParentPIDVersion: new(uint32(7301))}, "zsh")
	insert("bash", api.Process{PID: 4600, PPID: 1, Path: "/bin/bash", ForkTimeNs: 2000}, "")
	insert("subshell", api.Process{PID: 4650, PPID: 4600, Path: "/bin/bash", ForkTimeNs: 2010, ExitTimeNs: exited(2030)}, "bash")
	daemon := api.Process{PID: 4700, PPID: 4650, Path: "/bin/sleep", ForkTimeNs: 2020}
	daemon.Reparented = true
	insert("daemon", daemon, "subshell")
	insert("reuser", api.Process{PID: 4650, PPID: 1, Path: "/usr/bin/caffeinate", ForkTimeNs: 2040}, "")
	return ids
}

var creatorWindow = api.TimeRange{FromNs: 0, ToNs: 10_000}

// spec:server-rest-api/process-forest-links-by-creator/the-creator-row-beats-a-reused-pid
// spec:server-rest-api/process-forest-links-by-creator/the-ancestor-walk-follows-creators-through-a-double-fork
func TestTreeCreatorEdges_ForestAndAncestorWalk(t *testing.T) {
	t.Parallel()
	store, _ := openProcessStore(t)
	host := "creator-" + t.Name()
	ids := creatorRows(t, store, host)
	q := graph.NewQuery(store)

	t.Run("the forest hangs each process under its creator", func(t *testing.T) {
		t.Parallel()
		tree, err := q.BuildTree(t.Context(), host, creatorWindow, 100, true, 0)
		require.NoError(t, err)
		parents := treeParents(tree.Roots)
		assert.Equal(t, ids["zsh"], parents[ids["child"]], "not python3, the newest row holding pid 500")
		assert.Equal(t, ids["subshell"], parents[ids["daemon"]], "not the process that reused pid 4650")
		assert.Equal(t, ids["bash"], parents[ids["subshell"]])
	})

	t.Run("a pinned process's ancestors are its creators", func(t *testing.T) {
		t.Parallel()
		// A page of one row holds only the newest process, so every ancestor comes from the walk.
		tree, err := q.BuildTree(t.Context(), host, creatorWindow, 1, true, ids["child"])
		require.NoError(t, err)
		parents := treeParents(tree.Roots)
		assert.Equal(t, ids["zsh"], parents[ids["child"]], "the walk reads the creator by id, not pid 500 at the child's fork")
		_, python3Read := parents[ids["python3"]]
		assert.False(t, python3Read, "python3 is not on the chain")

		daemonTree, err := q.BuildTree(t.Context(), host, creatorWindow, 1, true, ids["daemon"])
		require.NoError(t, err)
		daemonParents := treeParents(daemonTree.Roots)
		assert.Equal(t, ids["subshell"], daemonParents[ids["daemon"]])
		assert.Equal(t, ids["bash"], daemonParents[ids["subshell"]])
	})
}

// spec:server-rest-api/process-forest-links-by-creator/a-creator-outside-the-result-leaves-the-process-a-root
func TestTreeCreatorEdges_PrunedCreatorEndsTheChain(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	host := "creator-pruned-" + t.Name()
	ids := creatorRows(t, store, host)
	_, err := db.ExecContext(t.Context(), `DELETE FROM processes WHERE id = ?`, ids["zsh"])
	require.NoError(t, err)

	q := graph.NewQuery(store)
	for _, limit := range []int{100, 1} {
		tree, err := q.BuildTree(t.Context(), host, creatorWindow, limit, true, ids["child"])
		require.NoError(t, err)
		parents := treeParents(tree.Roots)
		parent, present := parents[ids["child"]]
		require.True(t, present, "the pinned process is in the result")
		assert.Zero(t, parent, "a pruned creator leaves the child a root rather than under python3 (limit %d)", limit)
	}
}

// spec:server-rest-api/process-forest-links-by-creator/a-chain-excludes-a-child-of-another-process
func TestTreeCreatorEdges_ChainDescendants(t *testing.T) {
	t.Parallel()
	store, _ := openProcessStore(t)
	host := "creator-chain-" + t.Name()
	ids := creatorRows(t, store, host)
	q := graph.NewQuery(store)

	python3Chain, err := q.BuildChainTree(t.Context(), host, creatorWindow, ids["python3"], true)
	require.NoError(t, err)
	_, childInPython3 := treeParents(python3Chain.Roots)[ids["child"]]
	assert.False(t, childInPython3, "the child forked inside python3's lifetime with ppid 500, but zsh created it")

	zshChain, err := q.BuildChainTree(t.Context(), host, creatorWindow, ids["zsh"], true)
	require.NoError(t, err)
	assert.Equal(t, ids["zsh"], treeParents(zshChain.Roots)[ids["child"]])
}

// Process detail reads a generation by its exact identity; the lineage comes with it, so a reader can follow the creator from there.
func TestTreeCreatorEdges_DetailReadCarriesTheLineage(t *testing.T) {
	t.Parallel()
	store, _ := openProcessStore(t)
	host := "creator-detail-" + t.Name()
	ids := creatorRows(t, store, host)
	got, err := store.GetProcessByPIDVersion(t.Context(), host, 600, 7360, 1650)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, new(ids["zsh"]), got.ParentProcessID)
	assert.Equal(t, new(uint32(7301)), got.ParentPIDVersion)
}
