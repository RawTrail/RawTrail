//go:build integration

package tests

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// A session's digest (issue #1429) groups its file events by directory and its lookups and named connections by domain, counts the
// paths it touched outside its project, marks the domains the host had not looked up before, and spreads its events over time.
//
// spec:server-rest-api/agent-session-digest/the-digest-describes-what-the-session-touched
// spec:server-rest-api/agent-session-digest/the-digest-carries-a-timeline-of-the-session
func TestEventArchive_ActivityDigest(t *testing.T) {
	t.Parallel()
	arch := openTestArchive(t)
	ctx := context.Background()
	base := time.Now().Add(-time.Hour).UnixNano()
	at := func(s int64) int64 { return base + s*int64(time.Second) }
	file := func(id string, s int64, payload string) visibilityapi.Event {
		return activityEvent(id, "h1", "file_activity", at(s), `{"pid":300,"pidversion":3,`+payload+`}`)
	}
	lookup := func(id string, s int64, pid int, name string) visibilityapi.Event {
		return activityEvent(id, "h1", "dns_query", at(s), `{"pid":`+strconv.Itoa(pid)+`,"pidversion":3,"query_name":"`+name+`"}`)
	}

	require.NoError(t, arch.Insert(ctx, []visibilityapi.Event{
		file("f1", 1, `"operation":"modify","path":"/p/src/a.go","coalesced_count":3`),
		file("f2", 2, `"operation":"create","path":"/p/src/b.go"`),
		file("f3", 30, `"operation":"modify","path":"/p/README"`),
		// Events of the scope before and after the span are described, but not on the timeline.
		file("f6", -5, `"operation":"create","path":"/p/src/c.go"`),
		file("f7", 150, `"operation":"modify","path":"/p/src/a.go"`),
		// A folded row standing for the project itself is not outside it.
		file("f5", 40, `"operation":"modify","path":"/p","rolled_up":true`),
		file("f4", 10, `"operation":"create","path":"/p/node_modules","rolled_up":true,"coalesced_count":10`),
		activityEvent("o1", "h1", "open", at(60), `{"pid":300,"pidversion":3,"path":"/etc/hosts","flags":1}`),
		lookup("d1", 5, 300, "api.github.com"),
		lookup("d2", 6, 300, "registry.npmjs.org."),
		lookup("d3", 7, 300, "Example.COM"),
		// Two schools under the public suffix k12.ak.us are two domains: the host's lookup of one before does not make the other old.
		lookup("d4", 8, 300, "www.lincoln.k12.ak.us"),
		lookup("h2", -600, 999, "juneau.k12.ak.us"),
		activityEvent("n1", "h1", "network_connect", at(80), `{"pid":300,"pidversion":3,"protocol":"tcp","remote_address":"140.82.1.1",`+
			`"remote_port":443,"remote_hostname":"api.github.com","coalesced_count":2}`),
		// A connection the agent could not name counts in the timeline, not under a domain.
		activityEvent("n2", "h1", "network_connect", at(81), `{"pid":300,"pidversion":3,"protocol":"tcp","remote_address":"10.0.0.1",`+
			`"remote_port":22}`),
		// The host looked a name under github.com up before the session, from another process: github.com is not new.
		lookup("h1", -600, 999, "gist.github.com"),
		// Another generation's events are not the session's.
		activityEvent("x1", "h1", "file_activity", at(3), `{"pid":300,"pidversion":9,"operation":"create","path":"/q/other.go"}`),
		activityEvent("x2", "h1", "dns_query", at(4), `{"pid":300,"pidversion":9,"query_name":"other.org"}`),
	}))

	filter := visibilityapi.DigestFilter{
		ActivityFilter: visibilityapi.ActivityFilter{HostID: "h1", Generations: []visibilityapi.ProcessGeneration{{PID: 300, PIDVersion: 3}}},
		Project:        "/p/", StartNs: at(0), EndNs: at(99), Buckets: 4,
	}
	var got visibilityapi.ActivityDigest
	require.Eventually(t, func() bool {
		var err error
		got, err = arch.ActivityDigest(ctx, filter)
		return err == nil && len(got.Domains) == 4
	}, 5*time.Second, 100*time.Millisecond)

	assert.Equal(t, visibilityapi.ActivityDigest{
		Directories: []visibilityapi.DirectoryActivity{
			{Directory: "/p/node_modules", Paths: 1, ByOperation: map[string]int64{"create": 10}},
			{Directory: "/p/src", Paths: 3, ByOperation: map[string]int64{"modify": 4, "create": 2}},
			{Directory: "/p", Paths: 2, ByOperation: map[string]int64{"modify": 2}},
			{Directory: "/etc", Paths: 1, ByOperation: map[string]int64{"open": 1}},
		},
		// /etc/hosts was read, not changed, so nothing outside the project was.
		Files: visibilityapi.FileDigest{Changed: 6},
		Domains: []visibilityapi.DomainActivity{
			{Domain: "github.com", Lookups: 1, Connections: 2},
			{Domain: "example.com", Lookups: 1, New: true},
			{Domain: "lincoln.k12.ak.us", Lookups: 1, New: true},
			{Domain: "npmjs.org", Lookups: 1, New: true},
		},
		// The span widens from [0s, 99s] to the scope's first and last events, [-5s, 150s], in buckets 38.75s wide: the writes, lookups
		// and README of the first 38 seconds, the folded project and /etc/hosts, the two connections, and the last write.
		Timeline:         []int64{1 + 3 + 1 + 10 + 4 + 1, 1 + 1, 2 + 1, 1},
		TimelineStartNs:  at(-5),
		TimelineBucketNs: (at(150)-at(-5))/4 + 1,
	}, got)

	t.Run("a scope with no generation describes nothing", func(t *testing.T) {
		t.Parallel()
		empty, err := arch.ActivityDigest(ctx, visibilityapi.DigestFilter{
			ActivityFilter: visibilityapi.ActivityFilter{HostID: "h1"}, StartNs: 100, EndNs: 199, Buckets: 2,
		})
		require.NoError(t, err)
		assert.Equal(t, visibilityapi.ActivityDigest{
			Directories: []visibilityapi.DirectoryActivity{}, Domains: []visibilityapi.DomainActivity{}, Timeline: []int64{0, 0},
			TimelineStartNs: 100, TimelineBucketNs: 50,
		}, empty, "over the filter's own span")
	})

	t.Run("a scope with no events has a timeline over the filter's span", func(t *testing.T) {
		t.Parallel()
		quiet, err := arch.ActivityDigest(ctx, visibilityapi.DigestFilter{
			ActivityFilter: visibilityapi.ActivityFilter{HostID: "h1", Generations: []visibilityapi.ProcessGeneration{{PID: 777, PIDVersion: 1}}},
			StartNs:        at(0), EndNs: at(99), Buckets: 4,
		})
		require.NoError(t, err)
		assert.Equal(t, []int64{0, 0, 0, 0}, quiet.Timeline)
		assert.Equal(t, [2]int64{at(0), (at(99)-at(0))/4 + 1}, [2]int64{quiet.TimelineStartNs, quiet.TimelineBucketNs})
	})

	t.Run("without a project nothing is outside it", func(t *testing.T) {
		t.Parallel()
		unknown := filter
		unknown.Project = ""
		got, err := arch.ActivityDigest(ctx, unknown)
		require.NoError(t, err)
		assert.Zero(t, got.Files.OutsideProject)
	})
}

// A session's file counts leave out the files Claude Code keeps for itself, count apart the temporary files it created and removed, and
// count the files changed outside its project. Opens and socket connects change nothing.
//
// spec:server-rest-api/agent-session-digest/the-digest-counts-the-files-the-session-changed
func TestEventArchive_ActivityDigestFiles(t *testing.T) {
	t.Parallel()
	arch := openTestArchive(t)
	ctx := context.Background()
	base := time.Now().Add(-time.Hour).UnixNano()
	n := 0
	file := func(payload string) visibilityapi.Event {
		n++
		return activityEvent("f"+strconv.Itoa(n), "h1", "file_activity", base+int64(n), `{"pid":300,"pidversion":3,`+payload+`}`)
	}
	require.NoError(t, arch.Insert(ctx, []visibilityapi.Event{
		file(`"operation":"modify","path":"/p/a.go"`),
		file(`"operation":"create","path":"/Users/dev/notes.txt"`),
		// A scratch file made and removed, and an atomic write: the temporary file renamed over its target, which is what changed.
		file(`"operation":"create","path":"/private/var/folders/x/T/tmp1"`),
		file(`"operation":"delete","path":"/private/var/folders/x/T/tmp1"`),
		file(`"operation":"create","path":"/p/b.go.tmp.123"`),
		file(`"operation":"rename","path":"/p/b.go","source_path":"/p/b.go.tmp.123"`),
		// A file that was there before and was renamed: its new name changed, its old name is not a file of the session's.
		file(`"operation":"rename","path":"/p/new.txt","source_path":"/p/old.txt"`),
		// Claude Code's own files, a scratch one among them, are counted apart.
		file(`"operation":"modify","path":"/Users/dev/.claude/projects/-p/s.jsonl"`),
		file(`"operation":"create","path":"/Users/dev/.claude.json.tmp.1"`),
		file(`"operation":"modify","path":"/private/tmp/claude-501/-p/s/tasks/a.output"`),
		file(`"operation":"create","path":"/private/tmp/claude-4b72-cwd"`),
		file(`"operation":"delete","path":"/private/tmp/claude-4b72-cwd"`),
		file(`"operation":"create","path":"/Users/dev/Library/Caches/claude-cli-nodejs/-p/log.jsonl"`),
		// Names that only look like Claude Code's are the session's own changes: these two, and .claude.jsonnet below.
		file(`"operation":"create","path":"/Users/dev/.claudette/x"`),
		file(`"operation":"create","path":"/private/tmp/claude-notes.txt"`),
		file(`"operation":"create","path":"/Users/dev/.claude.jsonnet"`),
		// A file deleted and made again changed, however it began; so did a directory a rolled-up create and delete both name, whose
		// rows stand for different files.
		file(`"operation":"delete","path":"/Users/dev/report.txt"`),
		file(`"operation":"create","path":"/Users/dev/report.txt"`),
		file(`"operation":"create","path":"/p/node_modules","rolled_up":true,"coalesced_count":4`),
		file(`"operation":"delete","path":"/p/node_modules","rolled_up":true,"coalesced_count":2`),
		// A socket reached and a file read change nothing, Claude Code's own among them.
		file(`"operation":"connect","path":"/private/var/run/mDNSResponder"`),
		file(`"operation":"connect","path":"/private/tmp/cc-socks/501.sock"`),
		file(`"operation":"open","path":"/Users/dev/.claude/settings.json"`),
		activityEvent("o1", "h1", "open", base+100, `{"pid":300,"pidversion":3,"path":"/etc/hosts","flags":1}`),
	}))
	filter := visibilityapi.DigestFilter{
		ActivityFilter: visibilityapi.ActivityFilter{HostID: "h1", Generations: []visibilityapi.ProcessGeneration{{PID: 300, PIDVersion: 3}}},
		Project:        "/p/", StartNs: base, EndNs: base + 200, Buckets: 2,
	}
	// The archive shows inserted rows a part at a time, so the counts are read until they settle.
	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		got, err := arch.ActivityDigest(ctx, filter)
		require.NoError(c, err)
		assert.Equal(c, visibilityapi.FileDigest{Changed: 9, OutsideProject: 5, Temporary: 2, AgentFiles: 5}, got.Files)
	}, 5*time.Second, 100*time.Millisecond)

	t.Run("without a project nothing is outside it", func(t *testing.T) {
		t.Parallel()
		unknown := filter
		unknown.Project = ""
		got, err := arch.ActivityDigest(ctx, unknown)
		require.NoError(t, err)
		assert.Equal(t, visibilityapi.FileDigest{Changed: 9, Temporary: 2, AgentFiles: 5}, got.Files)
	})
}

// The busiest directories and domains are named, and the digest says when there were more.
func TestEventArchive_ActivityDigestCutsItsLists(t *testing.T) {
	t.Parallel()
	arch := openTestArchive(t)
	ctx := context.Background()
	base := time.Now().Add(-time.Hour).UnixNano()
	var events []visibilityapi.Event
	for i := range 60 {
		n := strconv.Itoa(i)
		events = append(events,
			activityEvent("f"+n, "h1", "file_activity", base+int64(i), `{"pid":300,"pidversion":3,"operation":"create","path":"/d`+n+`/f"}`),
			activityEvent("d"+n, "h1", "dns_query", base+int64(i), `{"pid":300,"pidversion":3,"query_name":"host`+n+`.test"}`))
	}
	require.NoError(t, arch.Insert(ctx, events))
	filter := visibilityapi.DigestFilter{
		ActivityFilter: visibilityapi.ActivityFilter{HostID: "h1", Generations: []visibilityapi.ProcessGeneration{{PID: 300, PIDVersion: 3}}},
		StartNs:        base, EndNs: base + 59, Buckets: 1,
	}
	var got visibilityapi.ActivityDigest
	require.Eventually(t, func() bool {
		var err error
		got, err = arch.ActivityDigest(ctx, filter)
		return err == nil && got.DomainsTruncated
	}, 5*time.Second, 100*time.Millisecond)
	assert.Len(t, got.Directories, 20)
	assert.True(t, got.DirectoriesTruncated)
	assert.Len(t, got.Domains, 50)
	assert.Equal(t, []int64{120}, got.Timeline)
}

// A session's prompts are counted as they are listed: the session's own, on its host.
//
// spec:server-rest-api/agent-session-digest/the-digest-describes-what-the-session-touched
func TestEventArchive_AgentPromptCount(t *testing.T) {
	t.Parallel()
	arch := openTestArchive(t)
	ctx := context.Background()
	base := time.Now().UnixNano()
	prompt := func(id, host, session string, i int64) visibilityapi.Event {
		return activityEvent(id, host, "agent_prompt", base+i, `{"agent_session_id":"`+session+`","prompt_id":"`+id+`","text":"hi"}`)
	}
	require.NoError(t, arch.Insert(ctx, []visibilityapi.Event{
		prompt("p1", "h1", "s1", 1), prompt("p2", "h1", "s1", 2), prompt("p3", "h1", "s2", 3), prompt("p4", "h2", "s1", 4),
	}))
	var n int64
	require.Eventually(t, func() bool {
		var err error
		n, err = arch.AgentPromptCount(ctx, "h1", "s1")
		return err == nil && n == 2
	}, 5*time.Second, 100*time.Millisecond)
	page, err := arch.AgentPrompts(ctx, visibilityapi.AgentPromptFilter{HostID: "h1", SessionID: "s1", Limit: 10})
	require.NoError(t, err)
	assert.Len(t, page.Prompts, int(n), "the count is the list's")
}
