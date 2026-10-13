//go:build integration

// The alert chain's descendants read from the story index (issue #1282): one read of the story's candidates, walked in memory by the
// same rule as the per-process walk, so the chain matches the walk and is not cut short at its 500-descendant cap.

package tests

import (
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fleetdm/edr/server/detection/api"
	"github.com/fleetdm/edr/server/detection/internal/graph"
	"github.com/fleetdm/edr/server/detection/internal/mysql"
)

var wholeWindow = api.TimeRange{FromNs: 0, ToNs: 1 << 62}

// stampStory sets every row of the host into the story rooted at root, as #1281's builder would have.
func stampStory(t *testing.T, db *sqlx.DB, host string, root int64) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `UPDATE processes SET story_root_id = ? WHERE host_id = ?`, root, host)
	require.NoError(t, err)
}

// pidParents maps each process in a forest to its parent's pid, so two forests built under different row ids compare.
func pidParents(roots []api.ProcessNode) map[int]int {
	out := map[int]int{}
	var walk func(parent int, nodes []api.ProcessNode)
	walk = func(parent int, nodes []api.ProcessNode) {
		for _, n := range nodes {
			out[n.PID] = parent
			walk(n.PID, n.Children)
		}
	}
	walk(0, roots)
	return out
}

// chainFixture seeds a tool call with descendants, a child forked before its creator's row was recorded (so it carries neither a
// creator link nor a story root, as the builder leaves it), a process that reused a child's pid after that child exited, and an
// unrelated process on the host.
func chainFixture(t *testing.T, store *mysql.Store, host string) int64 {
	t.Helper()
	ids := seedSession(t, store, host, sessionOne, 4, 6)
	insert := func(p api.Process) {
		p.HostID = host
		_, err := store.InsertProcess(t.Context(), p)
		require.NoError(t, err)
	}
	insert(api.Process{PID: 1200, PPID: 1000, Path: "/bin/unlinked", ForkTimeNs: 1500})
	insert(api.Process{PID: 1300, PPID: 1, Path: "/usr/libexec/unrelated", ForkTimeNs: 1600})
	// A descendant that exited, and a process forked after that naming its pid: the pid's next holder created it.
	insert(api.Process{PID: 1400, PPID: 1000, Path: "/bin/exited", ForkTimeNs: 1700, ExitTimeNs: new(int64(1800))})
	insert(api.Process{PID: 1401, PPID: 1400, Path: "/bin/reused", ForkTimeNs: 1900})
	// A pid held by an unrelated process first, whose child forked before a descendant took the pid over.
	insert(api.Process{PID: 1500, PPID: 1, Path: "/bin/earlier", ForkTimeNs: 1100, ExitTimeNs: new(int64(1150))})
	insert(api.Process{PID: 1501, PPID: 1500, Path: "/bin/earlier-child", ForkTimeNs: 1120})
	insert(api.Process{PID: 1500, PPID: 1000, Path: "/bin/later", ForkTimeNs: 1200})
	return ids[0]
}

// spec:server-rest-api/alert-chain-descendants-by-story/the-story-read-matches-the-walk
func TestChainStory_MatchesTheWalk(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	q := graph.NewQuery(store)
	stamped, walked := "chain-stamped-"+t.Name(), "chain-walked-"+t.Name()
	stampedShell := chainFixture(t, store, stamped)
	walkedShell := chainFixture(t, store, walked)
	stampStory(t, db, stamped, stampedShell)
	// The child forked before its creator's row was recorded carries no story root, as the builder leaves it.
	_, err := db.ExecContext(t.Context(), `UPDATE processes SET story_root_id = NULL WHERE host_id = ? AND pid = 1200`, stamped)
	require.NoError(t, err)

	fromStory, err := q.BuildChainTree(t.Context(), stamped, wholeWindow, stampedShell, true)
	require.NoError(t, err)
	fromWalk, err := q.BuildChainTree(t.Context(), walked, wholeWindow, walkedShell, true)
	require.NoError(t, err)
	require.False(t, fromWalk.Truncated)
	assert.Equal(t, pidParents(fromWalk.Roots), pidParents(fromStory.Roots))
	assert.Equal(t, 1000, pidParents(fromStory.Roots)[1200], "an unstamped child is found by its ppid, as the walk finds it")
	assert.NotContains(t, pidParents(fromStory.Roots), 1300, "an unrelated process stays out")
	assert.NotContains(t, pidParents(fromStory.Roots), 1401, "forked after its named parent exited")
	assert.NotContains(t, pidParents(fromStory.Roots), 1501, "forked before the descendant held its pid")
}

// spec:server-rest-api/alert-chain-descendants-by-story/a-chain-past-the-walk-s-cap-is-read-whole
func TestChainStory_PastTheWalkCap(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	q := graph.NewQuery(store)
	host := "chain-big-" + t.Name()
	ids := seedSession(t, store, host, sessionOne, 20, 31)
	stampStory(t, db, host, ids[0])
	db.SetMaxOpenConns(1)
	var name string
	var before, after int64
	require.NoError(t, db.QueryRowContext(t.Context(), `SHOW SESSION STATUS LIKE 'Questions'`).Scan(&name, &before))

	res, err := q.BuildChainTree(t.Context(), host, wholeWindow, ids[0], true)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(t.Context(), `SHOW SESSION STATUS LIKE 'Questions'`).Scan(&name, &after))
	assert.False(t, res.Truncated, "the walk's 500-descendant cap does not apply")
	assert.Equal(t, int64(len(ids)), res.Returned)
	// The pinned row, its one ancestor lookup (the shell's creator is not stored), the candidate read, and the status query.
	assert.Equal(t, int64(4), after-before)
}
