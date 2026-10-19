//go:build integration

// Inferred launches (issue #1286): an open or launchctl exec, then within seconds a launchd child running what it named.

package tests

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/internal/graph"
	"github.com/rawtrail/rawtrail/server/detection/internal/mysql"
)

const launchSession = "2757d523-59a6-4fe2-a19c-4fbdbb4fef56"

// launchSeed is the edr-dev capture's shape (Claude Code 2.1.296, macOS 26.3): a tool call ran `open -a TextEdit`, another
// `launchctl submit -- /bin/sleep 61`, and launchd started each through an xpcproxy row re-exec'd into the program. TextEdit inherited
// the agent's environment, so it carries the session; the launchctl job does not. A second `open -a TextEdit` comes later, with
// decoys: a launchd child that runs something else, and a TextEdit started too late to be the first open's.
type launchSeed struct {
	open, textEdit, launchctl, sleep, sleepChild, xpcSleep, laterOpen, laterTextEdit int64
}

func at(s float64) int64 { return int64(1_791_958_060e9) + int64(s*float64(time.Second)) }

func seedLaunches(t *testing.T, store *mysql.Store, db *sqlx.DB, host string) launchSeed {
	t.Helper()
	session := api.AgentSession{AgentSessionID: new(launchSession)}
	insert := func(p api.Process) int64 {
		p.HostID = host
		id, err := store.InsertProcess(t.Context(), p)
		require.NoError(t, err)
		return id
	}
	args := func(a ...string) api.NullRawJSON {
		raw, err := json.Marshal(a)
		require.NoError(t, err)
		return raw
	}
	// launchdChild inserts a launchd-started program the way the graph builder stores it: the xpcproxy row, its story's root, then
	// the program as a re-exec of the same pid.
	launchdChild := func(pid int, s float64, exe string, agent api.AgentSession) (xpc, program int64) {
		xpc = insert(api.Process{PID: pid, PPID: 1, Path: "/usr/libexec/xpcproxy", ForkTimeNs: at(s), ExecTimeNs: new(at(s))})
		_, err := db.ExecContext(t.Context(), "UPDATE processes SET story_root_id = id WHERE id = ?", xpc)
		require.NoError(t, err)
		program = insert(api.Process{PID: pid, PPID: 1, Path: exe, ForkTimeNs: at(s), ExecTimeNs: new(at(s) + 1), PreviousExecID: &xpc,
			StoryRootID: &xpc, AgentSession: agent})
		return xpc, program
	}
	var seed launchSeed
	shell := insert(api.Process{PID: 22795, PPID: 22518, Path: "/bin/zsh", ForkTimeNs: at(0), AgentSession: session})
	seed.open = insert(api.Process{PID: 22796, PPID: 22795, Path: "/usr/bin/open", Args: args("open", "-a", "TextEdit"),
		ForkTimeNs: at(1), ExecTimeNs: new(at(1)), ParentProcessID: &shell, AgentSession: session})
	// A second open of the app asks after the first, though its process forked before it, and launches nothing new: ordered by when
	// each asked, it must not claim the first open's launch.
	insert(api.Process{PID: 22797, PPID: 22795, Path: "/usr/bin/open", Args: args("open", "-a", "TextEdit"), ForkTimeNs: at(0.9),
		ExecTimeNs: new(at(1.1)), ParentProcessID: &shell, AgentSession: session})
	launchdChild(22799, 1.2, "/usr/libexec/other", api.AgentSession{})
	_, seed.textEdit = launchdChild(22798, 1.5, "/System/Applications/TextEdit.app/Contents/MacOS/TextEdit", session)
	// launchd started a sleep before the launchctl below asked for one, inside the first open's window: read for that window, it
	// must not be taken as the later ask's launch.
	launchdChild(22802, 2.0, "/bin/sleep", api.AgentSession{})
	seed.launchctl = insert(api.Process{PID: 22808, PPID: 22807, Path: "/bin/launchctl",
		Args: args("launchctl", "submit", "-l", "com.fleetdm.qa1286", "--", "/bin/sleep", "61"), ForkTimeNs: at(3), ExecTimeNs: new(at(3)),
		ParentProcessID: &shell, AgentSession: session})
	// The agent's own shell runs a sleep in the window too; it is not launchd's, so it is no launch.
	insert(api.Process{PID: 22850, PPID: 22795, Path: "/bin/sleep", ForkTimeNs: at(3.1), ExecTimeNs: new(at(3.1)), ParentProcessID: &shell,
		AgentSession: session})
	// A daemon launchd adopted, first seen at exec, has ppid 1 too but launchd did not start it, so it is no launch either.
	insert(api.Process{PID: 22851, PPID: 1, Path: "/bin/sleep", ForkTimeNs: at(3.15), ExecTimeNs: new(at(3.15)),
		ExecLineage: api.ExecLineage{Reparented: true}})
	seed.xpcSleep, seed.sleep = launchdChild(22809, 3.2, "/bin/sleep", api.AgentSession{})
	// A second sleep launchd starts later in the same window, as a relaunched job would: the earliest is the one asked for.
	launchdChild(22811, 4.5, "/bin/sleep", api.AgentSession{})
	// What the job started carries the session's markers, so the session holds it already: added under the job, it must not be
	// listed twice.
	seed.sleepChild = insert(api.Process{PID: 22810, PPID: 22809, Path: "/usr/bin/caffeinate", ForkTimeNs: at(4), ParentProcessID: &seed.sleep,
		StoryRootID: &seed.xpcSleep, AgentSession: session})
	seed.laterOpen = insert(api.Process{PID: 22900, PPID: 22795, Path: "/usr/bin/open", Args: args("open", "-a", "TextEdit"),
		ForkTimeNs: at(20), ExecTimeNs: new(at(20)), ParentProcessID: &shell, AgentSession: session})
	_, seed.laterTextEdit = launchdChild(22901, 20.5, "/System/Applications/TextEdit.app/Contents/MacOS/TextEdit", session)
	return seed
}

func forestIDs(roots []api.ProcessNode) map[int64]int {
	seen := map[int64]int{}
	var walk func([]api.ProcessNode)
	walk = func(nodes []api.ProcessNode) {
		for _, n := range nodes {
			seen[n.ID]++
			walk(n.Children)
		}
	}
	walk(roots)
	return seen
}

// spec:server-rest-api/inferred-launch-links/a-session-read-links-and-includes-what-its-launchers-started
func TestLaunchLinks_SessionRead(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	host := "launch-" + t.Name()
	seed := seedLaunches(t, store, db, host)

	res, err := graph.NewQuery(store).BuildScopedTree(t.Context(), host, api.TreeScope{AgentSessionID: launchSession}, 2000, true, 0)
	require.NoError(t, err)
	assert.Equal(t, []api.ProcessLink{
		{From: seed.open, To: seed.textEdit, Kind: api.ProcessLinkInferredLaunch},
		{From: seed.launchctl, To: seed.sleep, Kind: api.ProcessLinkInferredLaunch},
		{From: seed.laterOpen, To: seed.laterTextEdit, Kind: api.ProcessLinkInferredLaunch},
	}, res.Links, "each launcher to its own launch: not the decoy, not the other open's TextEdit")
	ids := forestIDs(res.Roots)
	assert.Equal(t, 1, ids[seed.sleep], "the unmarked launchctl job joins the session")
	assert.Equal(t, 1, ids[seed.sleepChild], "with what it started")
	assert.Equal(t, 1, ids[seed.textEdit], "the marked TextEdit is listed once")
	assert.Zero(t, ids[seed.xpcSleep], "launchd's xpcproxy is plumbing, not what the launcher asked for")
	// The shell, the three opens, both TextEdits, launchctl, the shell's own sleep and the job's marked child: the session's own rows.
	// The launched job is added beside them.
	assert.Equal(t, int64(9), res.Returned)
	assert.False(t, res.Truncated)

	// The default read folds the shell's identical `open -a TextEdit` leaves, but never a link's end: a client draws the edge by id.
	folded, err := graph.NewQuery(store).BuildScopedTree(t.Context(), host, api.TreeScope{AgentSessionID: launchSession}, 2000, false, 0)
	require.NoError(t, err)
	foldedIDs := forestIDs(folded.Roots)
	for _, l := range folded.Links {
		assert.Equal(t, 1, foldedIDs[l.From], "launcher %d", l.From)
		assert.Equal(t, 1, foldedIDs[l.To], "launched %d", l.To)
	}
}

// spec:server-rest-api/inferred-launch-links/a-window-read-links-only-what-it-holds
func TestLaunchLinks_WindowRead(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	host := "launch-" + t.Name()
	seed := seedLaunches(t, store, db, host)
	q := graph.NewQuery(store)

	whole, err := q.BuildTree(t.Context(), host, api.TimeRange{FromNs: at(0), ToNs: at(30)}, 2000, true, 0)
	require.NoError(t, err)
	assert.Len(t, whole.Links, 3)

	// A window that ends before the launchctl job started holds the launcher but not the job: no link, and no row added.
	early, err := q.BuildTree(t.Context(), host, api.TimeRange{FromNs: at(0), ToNs: at(3.1)}, 2000, true, 0)
	require.NoError(t, err)
	assert.Equal(t, []api.ProcessLink{{From: seed.open, To: seed.textEdit, Kind: api.ProcessLinkInferredLaunch}}, early.Links)
	assert.Zero(t, forestIDs(early.Roots)[seed.sleep])
}

// A launched program whose story is larger than a read adds is cut, and the read says so.
func TestLaunchLinks_LaunchedStoryCutIsReported(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	host := "launch-" + t.Name()
	seed := seedLaunches(t, store, db, host)
	_, err := db.ExecContext(t.Context(), `
		INSERT INTO processes (host_id, pid, ppid, path, fork_time_ns, parent_process_id, story_root_id)
		WITH RECURSIVE n (i) AS (SELECT 0 UNION ALL SELECT i + 1 FROM n WHERE i < 49)
		SELECT ?, 30000 + a.i * 50 + b.i, 22809, '/bin/date', ? + a.i * 50 + b.i, ?, ?
		FROM n AS a CROSS JOIN n AS b WHERE a.i * 50 + b.i < 2001`, host, at(5), seed.sleep, seed.xpcSleep)
	require.NoError(t, err)

	res, err := graph.NewQuery(store).BuildScopedTree(t.Context(), host, api.TreeScope{AgentSessionID: launchSession}, 2000, true, 0)
	require.NoError(t, err)
	assert.True(t, res.Truncated, "the launched job's descendants were cut")
	assert.Equal(t, 1, forestIDs(res.Roots)[seed.sleep])
}

// A window crowded with launchd children cannot use up the candidates another launcher's window needs.
func TestLaunchLinks_BusyWindowDoesNotStarveAnother(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	host := "launch-" + t.Name()
	seed := seedLaunches(t, store, db, host)
	// 201 unrelated launchd children inside the first open's window, all before the launchctl asks.
	_, err := db.ExecContext(t.Context(), `
		INSERT INTO processes (host_id, pid, ppid, path, fork_time_ns)
		WITH RECURSIVE n (i) AS (SELECT 0 UNION ALL SELECT i + 1 FROM n WHERE i < 200)
		SELECT ?, 40000 + i, 1, '/usr/libexec/busy', ? + i FROM n`, host, at(1.05))
	require.NoError(t, err)

	res, err := graph.NewQuery(store).BuildScopedTree(t.Context(), host, api.TreeScope{AgentSessionID: launchSession}, 2000, true, 0)
	require.NoError(t, err)
	assert.Contains(t, res.Links, api.ProcessLink{From: seed.launchctl, To: seed.sleep, Kind: api.ProcessLinkInferredLaunch})
	assert.Contains(t, res.Links, api.ProcessLink{From: seed.open, To: seed.textEdit, Kind: api.ProcessLinkInferredLaunch})
}
