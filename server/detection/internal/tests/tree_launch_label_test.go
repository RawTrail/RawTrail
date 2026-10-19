//go:build integration

// Inferred launches by job label and bundle id (issue #1341): `launchctl kickstart`/`start` name a job's label, which launchd's xpcproxy
// row for the job carries, and `open -b` names a bundle id, which the launched app's code-signing identifier is.

package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/internal/graph"
)

const labelSession = "f4b1b99e-0000-4000-8000-000000001341"

// spec:server-rest-api/launches-by-label-and-bundle-id/a-session-read-links-a-kickstart-a-start-and-an-open-by-bundle-id
func TestLaunchLinks_ByLabelAndBundleID(t *testing.T) {
	t.Parallel()
	store, _ := openProcessStore(t)
	host := "launch-label-" + t.Name()
	session := api.AgentSession{AgentSessionID: new(labelSession)}
	insert := func(p api.Process) int64 {
		p.HostID = host
		id, err := store.InsertProcess(t.Context(), p)
		require.NoError(t, err)
		return id
	}
	args := func(a ...string) api.NullRawJSON {
		raw := `[`
		for i, s := range a {
			if i > 0 {
				raw += ","
			}
			raw += `"` + s + `"`
		}
		return api.NullRawJSON(raw + `]`)
	}
	// job inserts what launchd stores for a job start: the xpcproxy row naming the label, run as root, then the program re-exec'd
	// from it, run as the job's user.
	job := func(pid int, s float64, label, program string, uid int) (xpcRow, programRow int64) {
		xpcRow = insert(api.Process{PID: pid, PPID: 1, Path: "/usr/libexec/xpcproxy", Args: args("xpcproxy", label), ForkTimeNs: at(s),
			ExecTimeNs: new(at(s)), UID: new(0)})
		programRow = insert(api.Process{PID: pid, PPID: 1, Path: program, ForkTimeNs: at(s), ExecTimeNs: new(at(s) + 1), PreviousExecID: &xpcRow,
			UID: &uid})
		return xpcRow, programRow
	}
	shell := insert(api.Process{PID: 31273, PPID: 31240, Path: "/bin/zsh", ForkTimeNs: at(0), AgentSession: session, UID: new(501)})
	launcher := func(pid int, s float64, path string, argv ...string) int64 {
		return insert(api.Process{PID: pid, PPID: 31273, Path: path, Args: args(argv...), ForkTimeNs: at(s), ExecTimeNs: new(at(s)),
			ParentProcessID: &shell, AgentSession: session, UID: new(501)})
	}
	// kickstart -k, and the job's restart 8.3 s later: launchd throttles a restart for up to its ThrottleInterval (10 s by default;
	// 5.0 s on edr-dev), past the 5 s a path match allows. Another job starts between them.
	kickstart := launcher(31275, 21.7, "/bin/launchctl", "launchctl", "kickstart", "-k", "gui/501/com.fleetdm.qa1341.bootstrap")
	job(31300, 22, "com.fleetdm.other", "/bin/sleep", 501)
	// The same label in another logged-in user's session, starting first: not this kickstart's.
	_, otherUser := job(31301, 23, "com.fleetdm.qa1341.bootstrap", "/bin/sleep", 502)
	kickXPC, kickProgram := job(31383, 30.0, "com.fleetdm.qa1341.bootstrap", "/bin/sleep", 501)
	// start by label, and the same label started again past the window, which is not this start's.
	start := launcher(31400, 40, "/bin/launchctl", "launchctl", "start", "com.fleetdm.qa1341.start")
	_, startProgram := job(31401, 41, "com.fleetdm.qa1341.start", "/bin/sleep", 501)
	_, lateProgram := job(31402, 70, "com.fleetdm.qa1341.start", "/bin/sleep", 501)
	// A start of an empty label, as an unset shell variable gives, names no job: the launchd child after it is not its.
	launcher(31500, 80, "/bin/launchctl", "launchctl", "start", "")
	_, afterEmpty := job(31501, 81, "com.fleetdm.unrelated", "/bin/sleep", 501)
	// open -b, an app signed with another identifier first, then TextEdit, which carries the session as the capture's did.
	openB := launcher(31387, 29.6, "/usr/bin/open", "open", "-b", "com.apple.TextEdit")
	insert(api.Process{PID: 31388, PPID: 1, Path: "/System/Applications/Notes.app/Contents/MacOS/Notes", ForkTimeNs: at(29.7),
		ExecTimeNs: new(at(29.7)), CodeSigning: api.NullRawJSON(`{"signing_id":"com.apple.Notes"}`), UID: new(501)})
	textEdit := insert(api.Process{PID: 31389, PPID: 1, Path: "/System/Applications/TextEdit.app/Contents/MacOS/TextEdit", ForkTimeNs: at(29.8),
		ExecTimeNs: new(at(29.8)), CodeSigning: api.NullRawJSON(`{"signing_id":"com.apple.TextEdit"}`), AgentSession: session, UID: new(501)})

	res, err := graph.NewQuery(store).BuildScopedTree(t.Context(), host, api.TreeScope{AgentSessionID: labelSession}, 2000, true, 0)
	require.NoError(t, err)
	assert.ElementsMatch(t, []api.ProcessLink{
		{From: kickstart, To: kickProgram, Kind: api.ProcessLinkInferredLaunch},
		{From: openB, To: textEdit, Kind: api.ProcessLinkInferredLaunch},
		{From: start, To: startProgram, Kind: api.ProcessLinkInferredLaunch},
	}, res.Links, "the restarted job's program, the app signed with the bundle id, and the start's own job")
	ids := forestIDs(res.Roots)
	assert.Equal(t, 1, ids[kickProgram], "the unmarked job joins the session")
	assert.Zero(t, ids[kickXPC], "launchd's xpcproxy is plumbing")
	assert.Zero(t, ids[lateProgram], "a start of the same label past the window is not this one's")
	assert.Zero(t, ids[otherUser], "nor the same label in another user's session")
	assert.Zero(t, ids[afterEmpty], "and an empty label names nothing")
}
