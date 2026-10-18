//go:build integration

// Service links (issue #1340): a process launchd started that names a process in the read, by pid and generation, as responsible.

package tests

import (
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/internal/graph"
	"github.com/rawtrail/rawtrail/server/detection/internal/mysql"
)

const serviceSession = "1000e537-0000-4000-8000-000000001340"

// serviceSeed is the edr-dev capture's shape (Claude Code 2.1.300, macOS 26.3): a tool call ran `open -a TextEdit page.html`, and
// TextEdit, launched by launchd and carrying the session, asked launchd for its XPC services, each naming TextEdit with its
// generation as responsible and carrying no session. Decoys: a service naming TextEdit's pid under another generation, one naming
// another app, one started after TextEdit exited, and textutil's, which names the SSH login rather than anything in the session.
type serviceSeed struct {
	textEdit, attributedString, webContent, webContentChild, reusedPID, otherApp, afterExit, textutilService int64
}

func seedServices(t *testing.T, store *mysql.Store, db *sqlx.DB, host string) serviceSeed {
	t.Helper()
	session := api.AgentSession{AgentSessionID: new(serviceSession)}
	insert := func(p api.Process) int64 {
		p.HostID = host
		id, err := store.InsertProcess(t.Context(), p)
		require.NoError(t, err)
		return id
	}
	responsible := func(pid int, pidversion uint32) api.ExecLineage {
		return api.ExecLineage{ResponsiblePID: &pid, ResponsiblePIDVersion: &pidversion}
	}
	// service inserts a launchd-started program the way the graph builder stores it: the xpcproxy row, its story's root, then the
	// program as a re-exec of the same pid, whose exec names the responsible process.
	service := func(pid int, pidversion uint32, s float64, exe string, lineage api.ExecLineage, agent api.AgentSession) (xpc, program int64) {
		xpc = insert(api.Process{PID: pid, PPID: 1, Path: "/usr/libexec/xpcproxy", ForkTimeNs: at(s), ExecTimeNs: new(at(s))})
		_, err := db.ExecContext(t.Context(), "UPDATE processes SET story_root_id = id WHERE id = ?", xpc)
		require.NoError(t, err)
		return xpc, insert(api.Process{PID: pid, PPID: 1, PIDVersion: new(pidversion), Path: exe, ForkTimeNs: at(s), ExecTimeNs: new(at(s) + 1),
			PreviousExecID: &xpc, StoryRootID: &xpc, AgentSession: agent, ExecLineage: lineage})
	}
	var seed serviceSeed
	shell := insert(api.Process{PID: 26938, PPID: 26937, Path: "/bin/zsh", ForkTimeNs: at(0), AgentSession: session,
		ExecLineage: responsible(26841, 10281100)})
	// textutil, run from the shell, names the SSH login as responsible, and so does the service it asked for: nothing links it.
	insert(api.Process{PID: 26939, PPID: 26938, Path: "/usr/bin/textutil", ForkTimeNs: at(0.5), ExecTimeNs: new(at(0.5)),
		ParentProcessID: &shell, AgentSession: session, ExecLineage: responsible(26841, 10281100)})
	_, seed.textutilService = service(26940, 10281150, 0.6, "/System/Library/PrivateFrameworks/X.framework/nsattributedstringagent",
		responsible(26841, 10281100), api.AgentSession{})
	// TextEdit, launched by launchd and responsible for itself, exits at 30s.
	textEditPV := uint32(10281301)
	_, seed.textEdit = service(26941, textEditPV, 1, "/System/Applications/TextEdit.app/Contents/MacOS/TextEdit", responsible(26941, textEditPV),
		session)
	_, err := db.ExecContext(t.Context(), "UPDATE processes SET exit_time_ns = ? WHERE id = ?", at(30), seed.textEdit)
	require.NoError(t, err)
	_, seed.attributedString = service(26962, 10281320, 2, "/System/Library/PrivateFrameworks/X.framework/nsattributedstringagent",
		responsible(26941, textEditPV), api.AgentSession{})
	webContentXPC, webContent := service(26965, 10281323, 2.1, "/System/Library/Frameworks/WebKit.framework/com.apple.WebKit.WebContent",
		responsible(26941, textEditPV), api.AgentSession{})
	seed.webContent = webContent
	// What a service starts is its own descendant, in the story its launch began, so it comes with the service.
	seed.webContentChild = insert(api.Process{PID: 26966, PPID: 26965, Path: "/usr/libexec/helper", ForkTimeNs: at(2.2),
		ParentProcessID: &seed.webContent, StoryRootID: &webContentXPC})
	_, seed.reusedPID = service(26970, 10281330, 3, "/usr/libexec/decoy", responsible(26941, textEditPV+99), api.AgentSession{})
	_, seed.otherApp = service(26971, 10281331, 3.1, "/usr/libexec/decoy", responsible(25000, 10200000), api.AgentSession{})
	_, seed.afterExit = service(26972, 10281332, 31, "/usr/libexec/decoy", responsible(26941, textEditPV), api.AgentSession{})
	return seed
}

// spec:server-rest-api/responsible-process-links/a-session-read-links-and-includes-the-services-of-its-apps
func TestServiceLinks_SessionRead(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	host := "service-" + t.Name()
	seed := seedServices(t, store, db, host)

	res, err := graph.NewQuery(store).BuildScopedTree(t.Context(), host, api.TreeScope{AgentSessionID: serviceSession}, 2000, true, 0)
	require.NoError(t, err)
	assert.Equal(t, []api.ProcessLink{
		{From: seed.textEdit, To: seed.attributedString, Kind: api.ProcessLinkResponsibleProcess},
		{From: seed.textEdit, To: seed.webContent, Kind: api.ProcessLinkResponsibleProcess},
	}, res.Links, "only the services naming TextEdit's own generation within its lifetime")
	ids := forestIDs(res.Roots)
	assert.Equal(t, 1, ids[seed.attributedString], "an unmarked service joins the session")
	assert.Equal(t, 1, ids[seed.webContentChild], "with what it started")
	for name, id := range map[string]int64{"reused pid": seed.reusedPID, "another app": seed.otherApp, "after exit": seed.afterExit,
		"textutil's": seed.textutilService} {
		assert.Zero(t, ids[id], name)
	}
	assert.False(t, res.Truncated)

	// The default read folds identical leaves, but never a link's end.
	folded, err := graph.NewQuery(store).BuildScopedTree(t.Context(), host, api.TreeScope{AgentSessionID: serviceSession}, 2000, false, 0)
	require.NoError(t, err)
	foldedIDs := forestIDs(folded.Roots)
	assert.Equal(t, 1, foldedIDs[seed.attributedString])
	assert.Equal(t, 1, foldedIDs[seed.webContent])
}

// A window read links the services it holds both ends of and adds nothing.
func TestServiceLinks_WindowRead(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	host := "service-" + t.Name()
	seed := seedServices(t, store, db, host)

	res, err := graph.NewQuery(store).BuildTree(t.Context(), host, api.TimeRange{FromNs: at(0), ToNs: at(40)}, 2000, true, 0)
	require.NoError(t, err)
	assert.Contains(t, res.Links, api.ProcessLink{From: seed.textEdit, To: seed.webContent, Kind: api.ProcessLinkResponsibleProcess})
	assert.NotContains(t, res.Links, api.ProcessLink{From: seed.textEdit, To: seed.afterExit, Kind: api.ProcessLinkResponsibleProcess})
}

// An app with more services than are read reports the cut rather than passing for complete.
func TestServiceLinks_CutIsReported(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	host := "service-" + t.Name()
	seedServices(t, store, db, host)
	for i := range 200 {
		pid := 30000 + i
		pidversion := uint32(10300000 + i)
		rp, rpv := 26941, uint32(10281301)
		_, err := store.InsertProcess(t.Context(), api.Process{HostID: host, PID: pid, PPID: 1, PIDVersion: &pidversion, Path: "/usr/libexec/svc",
			ForkTimeNs: at(5) + int64(i), ExecTimeNs: new(at(5) + int64(i)), ExecLineage: api.ExecLineage{ResponsiblePID: &rp, ResponsiblePIDVersion: &rpv}})
		require.NoError(t, err)
	}
	res, err := graph.NewQuery(store).BuildScopedTree(t.Context(), host, api.TreeScope{AgentSessionID: serviceSession}, 5000, true, 0)
	require.NoError(t, err)
	assert.True(t, res.Truncated)
	assert.Len(t, res.Links, 200, "the first 200 services are linked")
}

// serviceRow inserts a launchd child naming (rpid, rpv) as responsible, exiting at exit when it is non-zero.
func serviceRow(t *testing.T, store *mysql.Store, db *sqlx.DB, host string, pid int, pidversion uint32, s, exit float64, rpid int, rpv uint32,
	reparented bool) int64 {
	t.Helper()
	id, err := store.InsertProcess(t.Context(), api.Process{HostID: host, PID: pid, PPID: 1, PIDVersion: &pidversion, Path: "/usr/libexec/svc",
		ForkTimeNs: at(s), ExecTimeNs: new(at(s)), ExecLineage: api.ExecLineage{ResponsiblePID: &rpid, ResponsiblePIDVersion: &rpv, Reparented: reparented}})
	require.NoError(t, err)
	if exit != 0 {
		_, err = db.ExecContext(t.Context(), "UPDATE processes SET exit_time_ns = ? WHERE id = ?", at(exit), id)
		require.NoError(t, err)
	}
	return id
}

// appRow inserts an app launchd started, responsible for itself, in the session, exiting at exit when it is non-zero.
func appRow(t *testing.T, store *mysql.Store, db *sqlx.DB, host string, pid int, pidversion uint32, s, exit float64) int64 {
	t.Helper()
	session := api.AgentSession{AgentSessionID: new(serviceSession)}
	id, err := store.InsertProcess(t.Context(), api.Process{HostID: host, PID: pid, PPID: 1, PIDVersion: &pidversion, Path: "/Applications/App.app/Contents/MacOS/App",
		ForkTimeNs: at(s), ExecTimeNs: new(at(s)), AgentSession: session, ExecLineage: api.ExecLineage{ResponsiblePID: &pid, ResponsiblePIDVersion: &pidversion}})
	require.NoError(t, err)
	if exit != 0 {
		_, err = db.ExecContext(t.Context(), "UPDATE processes SET exit_time_ns = ? WHERE id = ?", at(exit), id)
		require.NoError(t, err)
	}
	return id
}

// A window read links a service it holds however many services the app started before the window.
func TestServiceLinks_WindowReadOfALongRunningApp(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	host := "service-" + t.Name()
	app := appRow(t, store, db, host, 500, 9000, 1, 0)
	for i := range 210 {
		serviceRow(t, store, db, host, 1000+i, uint32(9100+i), 2+float64(i)/1000, 3, 500, 9000, false)
	}
	recent := serviceRow(t, store, db, host, 2000, 9500, 12, 0, 500, 9000, false)
	res, err := graph.NewQuery(store).BuildTree(t.Context(), host, api.TimeRange{FromNs: at(10), ToNs: at(20)}, 2000, true, 0)
	require.NoError(t, err)
	assert.Contains(t, res.Links, api.ProcessLink{From: app, To: recent, Kind: api.ProcessLinkResponsibleProcess})
}

// A session with more apps than are looked at says so.
func TestServiceLinks_TooManyAppsIsReported(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	host := "service-" + t.Name()
	for i := range 51 {
		appRow(t, store, db, host, 600+i, uint32(9600+i), 1+float64(i)/100, 0)
	}
	res, err := graph.NewQuery(store).BuildScopedTree(t.Context(), host, api.TreeScope{AgentSessionID: serviceSession}, 5000, true, 0)
	require.NoError(t, err)
	assert.True(t, res.Truncated)
}

// A generation counter restarts with the host, so two apps can share a pid and generation; a service belongs to the one alive at its
// start. A process launchd adopted rather than started is no service, whatever it names.
func TestServiceLinks_OwnerByLifetimeAndNoAdoptedServices(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	host := "service-" + t.Name()
	first := appRow(t, store, db, host, 700, 9700, 1, 10)
	second := appRow(t, store, db, host, 700, 9700, 20, 30)
	ofSecond := serviceRow(t, store, db, host, 701, 9701, 25, 0, 700, 9700, false)
	adopted := serviceRow(t, store, db, host, 702, 9702, 26, 0, 700, 9700, true)
	res, err := graph.NewQuery(store).BuildScopedTree(t.Context(), host, api.TreeScope{AgentSessionID: serviceSession}, 5000, true, 0)
	require.NoError(t, err)
	assert.Contains(t, res.Links, api.ProcessLink{From: second, To: ofSecond, Kind: api.ProcessLinkResponsibleProcess})
	assert.NotContains(t, res.Links, api.ProcessLink{From: first, To: ofSecond, Kind: api.ProcessLinkResponsibleProcess})
	assert.Zero(t, forestIDs(res.Roots)[adopted], "an adopted process is no service")

	window, err := graph.NewQuery(store).BuildTree(t.Context(), host, api.TimeRange{FromNs: at(0), ToNs: at(40)}, 2000, true, 0)
	require.NoError(t, err)
	assert.Contains(t, window.Links, api.ProcessLink{From: second, To: ofSecond, Kind: api.ProcessLinkResponsibleProcess})
	for _, l := range window.Links {
		assert.NotEqual(t, adopted, l.To, "the window read links no adopted process either")
	}
}

// Adopted processes are left out in the store, so however many name an app they cannot use up the services read for it.
func TestServiceLinks_AdoptedProcessesDoNotCrowdOutServices(t *testing.T) {
	t.Parallel()
	store, db := openProcessStore(t)
	host := "service-" + t.Name()
	app := appRow(t, store, db, host, 800, 9800, 1, 0)
	for i := range 201 {
		serviceRow(t, store, db, host, 3000+i, uint32(9900+i), 2+float64(i)/1000, 0, 800, 9800, true)
	}
	genuine := serviceRow(t, store, db, host, 4000, 10000, 5, 0, 800, 9800, false)
	res, err := graph.NewQuery(store).BuildScopedTree(t.Context(), host, api.TreeScope{AgentSessionID: serviceSession}, 5000, true, 0)
	require.NoError(t, err)
	assert.Equal(t, []api.ProcessLink{{From: app, To: genuine, Kind: api.ProcessLinkResponsibleProcess}}, res.Links)
	assert.False(t, res.Truncated)
}
