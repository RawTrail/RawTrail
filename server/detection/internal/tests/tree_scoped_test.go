//go:build integration

// Story and session tree reads (issue #1282): a whole scope by its index, where the chain read issues a query per process it descends
// into and stops at maxChainDescendants.

package tests

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/bootstrap"
	"github.com/rawtrail/rawtrail/server/detection/internal/graph"
	"github.com/rawtrail/rawtrail/server/detection/internal/mysql"
)

// seedSession inserts a tool call's shell and descendants levels deep, fanout wide at each level, all in session and story root,
// and returns the row ids in insertion order (the shell first). Every row links to its creator as the graph builder links them.
func seedSession(t *testing.T, store *mysql.Store, host, session string, levels, fanout int) []int64 {
	t.Helper()
	insert := func(pid, ppid int, fork int64, parent *int64) int64 {
		p := api.Process{HostID: host, PID: pid, PPID: ppid, Path: "/bin/p" + strconv.Itoa(pid), ForkTimeNs: fork,
			ParentProcessID: parent, AgentSession: api.AgentSession{AgentSessionID: &session, AgentName: new("claude-code")}}
		id, err := store.InsertProcess(t.Context(), p)
		require.NoError(t, err)
		return id
	}
	type node struct {
		id  int64
		pid int
	}
	shell := node{id: insert(1000, 900, 1000, nil), pid: 1000}
	ids := []int64{shell.id}
	frontier := []node{shell}
	pid, fork := 1001, int64(1001)
	for range levels {
		var next []node
		parent := frontier[0]
		for range fanout {
			child := node{id: insert(pid, parent.pid, fork, &parent.id), pid: pid}
			ids = append(ids, child.id)
			next = append(next, child)
			pid++
			fork++
		}
		frontier = next
	}
	return ids
}

func forestParents(roots []api.ProcessNode) map[int64]int64 {
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

// spec:server-rest-api/story-and-session-tree-reads/a-session-is-read-whole
// spec:server-rest-api/story-and-session-tree-reads/a-scope-reads-only-its-host
func TestScopedTree_SessionReadWhole(t *testing.T) {
	t.Parallel()
	store, _ := openProcessStore(t)
	q := graph.NewQuery(store)
	host := "scoped-" + t.Name()
	// 20 levels of 31: 621 processes under one tool call, past the chain walk's 500-descendant cap.
	ids := seedSession(t, store, host, sessionOne, 20, 31)
	seedSession(t, store, "other-"+host, sessionOne, 2, 3)

	res, err := q.BuildScopedTree(t.Context(), host, api.TreeScope{AgentSessionID: sessionOne}, 2000, true, 0)
	require.NoError(t, err)
	assert.False(t, res.Truncated)
	assert.Equal(t, int64(len(ids)), res.Returned)
	parents := forestParents(res.Roots)
	assert.Len(t, parents, len(ids), "every process of the session, and none from the other host")
	assert.Equal(t, int64(0), parents[ids[0]], "the tool call's shell is the root: its creator is outside the session")
	assert.Equal(t, ids[0], parents[ids[1]])

	chain, err := q.BuildChainTree(t.Context(), host, api.TimeRange{FromNs: 0, ToNs: 1 << 62}, ids[0], true)
	require.NoError(t, err)
	assert.True(t, chain.Truncated, "the walk stops at its cap on the same session")
}

// The indexed read answers what the walk answers, on a session the walk can read whole.
func TestScopedTree_MatchesTheChainWalk(t *testing.T) {
	t.Parallel()
	store, _ := openProcessStore(t)
	q := graph.NewQuery(store)
	host := "scoped-" + t.Name()
	ids := seedSession(t, store, host, sessionOne, 6, 8)

	scoped, err := q.BuildScopedTree(t.Context(), host, api.TreeScope{AgentSessionID: sessionOne}, 2000, true, 0)
	require.NoError(t, err)
	chain, err := q.BuildChainTree(t.Context(), host, api.TimeRange{FromNs: 0, ToNs: 1 << 62}, ids[0], true)
	require.NoError(t, err)
	require.False(t, chain.Truncated)
	assert.Equal(t, forestParents(chain.Roots), forestParents(scoped.Roots))
}

// spec:server-rest-api/story-and-session-tree-reads/a-scope-beyond-the-limit-reports-truncation
func TestScopedTree_StoryTruncation(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	q := graph.NewQuery(store)
	host := "scoped-" + t.Name()
	ids := seedSession(t, store, host, sessionOne, 3, 40)
	_, err := db.ExecContext(t.Context(), `UPDATE processes SET story_root_id = ? WHERE host_id = ?`, ids[0], host)
	require.NoError(t, err)

	res, err := q.BuildScopedTree(t.Context(), host, api.TreeScope{StoryRootID: ids[0]}, 50, true, 0)
	require.NoError(t, err)
	assert.True(t, res.Truncated)
	assert.Equal(t, int64(50), res.Returned)
	assert.Equal(t, int64(len(ids)), res.TotalMatched)
	parents := forestParents(res.Roots)
	for _, id := range ids[:50] {
		assert.Contains(t, parents, id, "the oldest processes fill the page")
	}
}

// The count behind a truncated session read counts that host's processes only, though the session id repeats on another.
func TestScopedTree_SessionCountIsPerHost(t *testing.T) {
	t.Parallel()
	store, _ := openProcessStore(t)
	q := graph.NewQuery(store)
	host := "scoped-" + t.Name()
	ids := seedSession(t, store, host, sessionOne, 2, 40)
	seedSession(t, store, "other-"+host, sessionOne, 3, 40)

	res, err := q.BuildScopedTree(t.Context(), host, api.TreeScope{AgentSessionID: sessionOne}, 10, true, 0)
	require.NoError(t, err)
	assert.True(t, res.Truncated)
	assert.Equal(t, int64(len(ids)), res.TotalMatched)
}

// spec:server-rest-api/story-and-session-tree-reads/a-session-is-read-whole
//
// The scoped read is one statement whatever the tree's depth, where the chain walk issues one per process it descends into. Counted
// on a single connection, so the session's Questions counter sees exactly what the read sent.
func TestScopedTree_OneQueryWhateverTheDepth(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	q := graph.NewQuery(store)
	host := "scoped-" + t.Name()
	ids := seedSession(t, store, host, sessionOne, 20, 5)
	db.SetMaxOpenConns(1)
	questions := func() int64 {
		var name string
		var n int64
		require.NoError(t, db.QueryRowContext(t.Context(), `SHOW SESSION STATUS LIKE 'Questions'`).Scan(&name, &n))
		return n
	}

	before := questions()
	res, err := q.BuildScopedTree(t.Context(), host, api.TreeScope{AgentSessionID: sessionOne}, 2000, true, 0)
	require.NoError(t, err)
	require.False(t, res.Truncated)
	assert.Equal(t, int64(2), questions()-before, "the scoped read, plus the status query itself")

	before = questions()
	_, err = q.BuildChainTree(t.Context(), host, api.TimeRange{FromNs: 0, ToNs: 1 << 62}, ids[0], true)
	require.NoError(t, err)
	assert.Greater(t, questions()-before, int64(20), "the walk issues a query per process it descends into")
}

// Through the service, as the handler calls it: without flatten, identical leaf siblings of a session fold into one aggregate.
func TestScopedTree_ServiceAggregatesSiblings(t *testing.T) {
	t.Parallel()
	d := newDetection(t, detectionOpts{mode: bootstrap.ModeFull})
	host := "scoped-" + t.Name()
	store := d.Store()
	session := api.AgentSession{AgentSessionID: new(sessionOne)}
	shell, err := store.InsertProcess(t.Context(), api.Process{HostID: host, PID: 1000, PPID: 900, Path: "/bin/zsh", ForkTimeNs: 1000,
		AgentSession: session})
	require.NoError(t, err)
	for i := range 12 {
		_, err := store.InsertProcess(t.Context(), api.Process{HostID: host, PID: 1001 + i, PPID: 1000, Path: "/usr/bin/grep",
			ForkTimeNs: int64(1001 + i), ParentProcessID: &shell, AgentSession: session})
		require.NoError(t, err)
	}

	res, err := d.Service().BuildScopedTree(t.Context(), host, api.TreeScope{AgentSessionID: sessionOne}, 2000, false, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(13), res.Returned, "counted before aggregation")
	require.Len(t, res.Roots, 1, "the tool call's shell")
	require.Len(t, res.Roots[0].Children, 1, "twelve identical leaves fold into one node")
	require.NotNil(t, res.Roots[0].Children[0].Aggregated)
	assert.Equal(t, 12, res.Roots[0].Children[0].Aggregated.Count)
}
