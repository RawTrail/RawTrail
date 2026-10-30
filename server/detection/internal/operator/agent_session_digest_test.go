package operator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/internal/sessioncommands"
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// programNode is a process of the program at path, with its children.
func programNode(id int64, path string, children ...api.ProcessNode) api.ProcessNode {
	return api.ProcessNode{Process: api.Process{ID: id, Path: path}, Children: children}
}

// spec:server-rest-api/agent-session-digest/the-digest-names-the-programs-the-commands-ran
func TestTopPrograms(t *testing.T) {
	t.Parallel()
	collapsed := programNode(7, "/usr/bin/sed")
	collapsed.Aggregated = &api.AggregatedSiblings{Count: 5}
	for _, tc := range []struct {
		name     string
		commands []sessioncommands.Command
		want     []programCount
	}{
		{name: "no commands", commands: nil, want: []programCount{}},
		{
			name: "the agent's wrapper shell is not counted, what it ran is",
			commands: []sessioncommands.Command{{
				FromAgent: true, RootID: 1,
				Roots: []api.ProcessNode{programNode(1, "/bin/zsh", programNode(2, "/usr/bin/git"), programNode(3, "/usr/bin/git"))},
			}},
			want: []programCount{{Program: "git", Count: 2}},
		},
		{
			name: "a command the agent did not wrap counts its root",
			commands: []sessioncommands.Command{{
				Roots: []api.ProcessNode{programNode(1, "/bin/zsh", programNode(2, "/usr/bin/git"))},
			}},
			want: []programCount{{Program: "git", Count: 1}, {Program: "zsh", Count: 1}},
		},
		{
			name: "a root that joined a wrapped command counts",
			commands: []sessioncommands.Command{{
				FromAgent: true, RootID: 1,
				Roots: []api.ProcessNode{programNode(1, "/bin/zsh"), programNode(4, "/usr/local/bin/node")},
			}},
			want: []programCount{{Program: "node", Count: 1}},
		},
		{
			name: "a collapsed group counts as its members, most run first",
			commands: []sessioncommands.Command{{
				Roots: []api.ProcessNode{programNode(6, "/usr/bin/find", collapsed)},
			}},
			want: []programCount{{Program: "sed", Count: 5}, {Program: "find", Count: 1}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, topPrograms(tc.commands))
		})
	}
}

func TestTopPrograms_NamesTheBusiestOnly(t *testing.T) {
	t.Parallel()
	var roots []api.ProcessNode
	for i := range digestPrograms + 3 {
		roots = append(roots, programNode(int64(i+1), "/bin/p"+string(rune('a'+i))))
	}
	roots = append(roots, programNode(100, "/bin/pz"), programNode(101, "/bin/pz"))
	got := topPrograms([]sessioncommands.Command{{Roots: roots}})
	assert.Len(t, got, digestPrograms)
	assert.Equal(t, programCount{Program: "pz", Count: 2}, got[0], "the most run leads")
	assert.Equal(t, "pa", got[1].Program, "ties are by name")
}

// A session is sighted at its marked execs: its first command forks before the first, and its last command runs past the last. The
// digest's span reaches both.
//
// spec:server-rest-api/agent-session-digest/the-digest-carries-a-timeline-of-the-session
func TestDigestFilter_SpansTheCommands(t *testing.T) {
	t.Parallel()
	project := "/Users/dev/app"
	session := &api.AgentSessionSummary{FirstSeenNs: 1000, LastSeenNs: 2000, CWD: &project}
	scope := visibilityapi.ActivityFilter{HostID: "h1"}
	commands := []sessioncommands.Command{{StartNs: 990, LastNs: 1500}, {StartNs: 1900, LastNs: 2600}}
	assert.Equal(t, visibilityapi.DigestFilter{ActivityFilter: scope, Project: project, StartNs: 990, EndNs: 2600, Buckets: digestBuckets},
		digestFilter(session, scope, commands))
	inside := []sessioncommands.Command{{StartNs: 1100, LastNs: 1200}}
	got := digestFilter(&api.AgentSessionSummary{FirstSeenNs: 1000, LastSeenNs: 2000}, scope, inside)
	assert.Equal(t, [2]int64{1000, 2000}, [2]int64{got.StartNs, got.EndNs}, "commands inside the sightings leave the span alone")
	assert.Empty(t, got.Project, "a session with no project names none")
}

func TestCommandTimeline(t *testing.T) {
	t.Parallel()
	activity := visibilityapi.ActivityDigest{Timeline: make([]int64, 4), TimelineStartNs: 1000, TimelineBucketNs: 25}
	at := func(ns ...int64) []sessioncommands.Command {
		out := make([]sessioncommands.Command, len(ns))
		for i, n := range ns {
			out[i].StartNs = n
		}
		return out
	}
	assert.Equal(t, []int64{2, 0, 1, 1}, commandTimeline(at(1000, 1024, 1050, 1099), activity), "the span's ends fall in the end buckets")
}

// Whatever the sightings and the commands, the digest's span holds every command, and over buckets split as the archive splits it
// each command is counted once, in the bucket whose interval holds it. The bucket is found by scanning the intervals, not by the
// division the code uses.
func TestCommandTimeline_CountsEachCommandInItsBucket(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		first := rapid.Int64Range(1<<20, 1<<40).Draw(t, "first")
		session := &api.AgentSessionSummary{FirstSeenNs: first, LastSeenNs: first + rapid.Int64Range(0, 1<<40).Draw(t, "sighted")}
		starts := rapid.SliceOf(rapid.Int64Range(session.FirstSeenNs-1<<20, session.LastSeenNs+1<<20)).Draw(t, "starts")
		commands := make([]sessioncommands.Command, len(starts))
		for i, s := range starts {
			commands[i] = sessioncommands.Command{StartNs: s, LastNs: s + rapid.Int64Range(0, 1<<20).Draw(t, "ran")}
		}
		filter := digestFilter(session, visibilityapi.ActivityFilter{}, commands)
		buckets := rapid.IntRange(1, 100).Draw(t, "buckets")
		activity := visibilityapi.ActivityDigest{
			Timeline: make([]int64, buckets), TimelineStartNs: filter.StartNs, TimelineBucketNs: (filter.EndNs-filter.StartNs)/int64(buckets) + 1,
		}
		want := make([]int64, buckets)
		for _, c := range commands {
			bucket := -1
			for b := range buckets {
				if lo := activity.TimelineStartNs + int64(b)*activity.TimelineBucketNs; c.StartNs >= lo && c.StartNs < lo+activity.TimelineBucketNs {
					bucket = b
				}
			}
			if bucket < 0 {
				t.Fatalf("no bucket of the span [%d, %d] holds the command at %d", filter.StartNs, filter.EndNs, c.StartNs)
			}
			want[bucket]++
		}
		assert.Equal(t, want, commandTimeline(commands, activity))
	})
}
