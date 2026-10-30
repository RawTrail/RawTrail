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
		OutsideProject: 1,
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
		assert.Zero(t, got.OutsideProject)
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
