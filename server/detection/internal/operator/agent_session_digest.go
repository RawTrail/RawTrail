package operator

import (
	"cmp"
	"path"
	"slices"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/internal/sessioncommands"
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// The digest's shape (issue #1429): how many programs it names, and how many buckets its timeline splits the session into.
const (
	digestPrograms = 10
	digestBuckets  = 48
)

// sessionDigest is a session described for its Summary (issue #1429): how many prompts it was sent, the programs its commands ran,
// what it touched by directory and domain, and its timeline. The timeline splits the session's span, from TimelineStartNs, into
// equal buckets TimelineBucketNs wide: Timeline counts the file, network and DNS events in each, and CommandTimeline the commands
// that started in each.
type sessionDigest struct {
	Prompts  int64          `json:"prompts"`
	Programs []programCount `json:"programs"`
	visibilityapi.ActivityDigest
	CommandTimeline []int64 `json:"command_timeline"`
}

// programCount is how many processes of one program the session's commands ran.
type programCount struct {
	Program string `json:"program"`
	Count   int    `json:"count"`
}

// digestFilter selects what the session's digest describes: its activity scope, its project, and its span split into the
// timeline's buckets. The span runs from the session's first sighting, or its first command's start when that is earlier, to its last
// sighting, or its last command's last activity when that is later: a session is sighted at its marked execs, which a command's
// fork precedes and its writes follow. The archive widens it further to the scope's own events.
func digestFilter(
	session *api.AgentSessionSummary, scope visibilityapi.ActivityFilter, commands []sessioncommands.Command,
) visibilityapi.DigestFilter {
	filter := visibilityapi.DigestFilter{
		ActivityFilter: scope, StartNs: session.FirstSeenNs, EndNs: session.LastSeenNs, Buckets: digestBuckets,
	}
	for _, c := range commands {
		filter.StartNs = min(filter.StartNs, c.StartNs)
		filter.EndNs = max(filter.EndNs, c.LastNs)
	}
	if session.CWD != nil {
		filter.Project = *session.CWD
	}
	return filter
}

// commandTimeline counts the commands that started in each bucket of the archive's timeline, which spans every command's start.
func commandTimeline(commands []sessioncommands.Command, activity visibilityapi.ActivityDigest) []int64 {
	timeline := make([]int64, len(activity.Timeline))
	for _, c := range commands {
		timeline[(c.StartNs-activity.TimelineStartNs)/activity.TimelineBucketNs]++
	}
	return timeline
}

// topPrograms counts the processes the commands ran by program, the base name of each one's executable, most run first and then by
// name. The shell that wraps a command the agent ran is the agent's plumbing, not a program it chose, so it is not counted; a
// collapsed group counts as its members.
func topPrograms(commands []sessioncommands.Command) []programCount {
	counts := map[string]int{}
	var walk func(nodes []api.ProcessNode)
	walk = func(nodes []api.ProcessNode) {
		for _, n := range nodes {
			members := 1
			if n.Aggregated != nil {
				members = n.Aggregated.Count
			}
			counts[path.Base(n.Path)] += members
			walk(n.Children)
		}
	}
	for _, c := range commands {
		for _, root := range c.Roots {
			if c.FromAgent && root.ID == c.RootID {
				walk(root.Children)
				continue
			}
			walk([]api.ProcessNode{root})
		}
	}
	out := make([]programCount, 0, len(counts))
	for program, n := range counts {
		out = append(out, programCount{Program: program, Count: n})
	}
	slices.SortFunc(out, func(a, b programCount) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Program, b.Program))
	})
	return out[:min(len(out), digestPrograms)]
}
