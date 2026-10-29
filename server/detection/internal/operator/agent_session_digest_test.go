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

// spec:server-rest-api/agent-session-digest/the-digest-carries-a-timeline-of-the-session
func TestCommandTimeline(t *testing.T) {
	t.Parallel()
	filter := visibilityapi.DigestFilter{StartNs: 1000, EndNs: 1099, Buckets: 4}
	at := func(ns ...int64) []sessioncommands.Command {
		out := make([]sessioncommands.Command, len(ns))
		for i, n := range ns {
			out[i].StartNs = n
		}
		return out
	}
	assert.Equal(t, int64(25), bucketWidth(filter))
	assert.Equal(t, []int64{2, 0, 1, 1}, commandTimeline(at(1000, 1024, 1050, 1099), filter), "the span's ends fall in the end buckets")
	assert.Equal(t, []int64{0, 0, 0, 0}, commandTimeline(at(999, 1100), filter), "a command outside the span is not counted")
}

// Whatever the span and the commands, each command inside the span is counted once, in the bucket whose interval holds it, and the
// buckets together cover the span; none outside the span is counted. The bucket is found by scanning the intervals, not by the
// division the code uses.
func TestCommandTimeline_CountsEachCommandInItsBucket(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		start := rapid.Int64Range(0, 1<<40).Draw(t, "start")
		filter := visibilityapi.DigestFilter{
			StartNs: start, EndNs: start + rapid.Int64Range(0, 1<<40).Draw(t, "span"), Buckets: rapid.IntRange(1, 100).Draw(t, "buckets"),
		}
		starts := rapid.SliceOf(rapid.Int64Range(filter.StartNs-1000, filter.EndNs+1000)).Draw(t, "starts")
		commands := make([]sessioncommands.Command, len(starts))
		want := make([]int64, filter.Buckets)
		width := bucketWidth(filter)
		for i, s := range starts {
			commands[i].StartNs = s
			if s < filter.StartNs || s > filter.EndNs {
				continue
			}
			bucket := -1
			for b := range filter.Buckets {
				if lo := filter.StartNs + int64(b)*width; s >= lo && s < lo+width {
					bucket = b
				}
			}
			if bucket < 0 {
				t.Fatalf("no bucket holds the command at %d", s)
			}
			want[bucket]++
		}
		assert.Equal(t, want, commandTimeline(commands, filter))
	})
}
