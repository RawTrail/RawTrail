package graph

import (
	"cmp"
	"context"
	"encoding/json"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/fleetdm/edr/server/detection/api"
)

// Inferred launches (issue #1286). A process launchd starts on a caller's behalf keeps no lineage to the caller: `open -a App` and
// `launchctl submit` both end in a launchd child with parent 1, its own process group, and responsibility for itself (#1276). What
// remains is a correlation: the open or launchctl exec, then within seconds a launchd child running the app or program it named.
const (
	// launchWindow is how long after the launcher's exec its launchd child may start. Launch Services answered within a second on
	// edr-dev; five leaves room for a cold app start without reaching far enough to pair unrelated launches.
	launchWindow = 5 * time.Second
	// maxLaunchers bounds how many launchers one read infers for, the oldest kept.
	maxLaunchers = 50
	// maxLaunchCandidates bounds the launchd children read for all of a read's launchers at once.
	maxLaunchCandidates = 2000
	// maxLaunchStory bounds the rows a scoped read adds for the programs its launchers started; past it the read reports truncation.
	maxLaunchStory = 2000
	// The launchers, by their canonical paths: a binary elsewhere that happens to be named open or launchctl is not Apple's.
	openPath      = "/usr/bin/open"
	launchctlPath = "/bin/launchctl"
)

// launchTarget returns what a launcher exec asked launchd to start, as a match over a candidate's executable path, and false for an
// exec that is no launcher or names nothing a path can match:
//
//   - `open -a App` or `open -a /path/App.app`, and `open /path/App.app`: the executable lies inside that bundle.
//   - `launchctl submit ... -- program [args]`: the executable is the program.
//
// `open -b bundle.id`, `launchctl load|bootstrap plist` and XPC hops name nothing a path matches and are not inferred.
func launchTarget(p api.Process) (func(string) bool, bool) {
	if p.Path != openPath && p.Path != launchctlPath {
		return nil, false
	}
	// The argv is stored as the JSON array the exec reported; one that does not decode names nothing to match.
	var args []string
	if json.Unmarshal(p.Args, &args) != nil || len(args) < 2 {
		return nil, false
	}
	if p.Path == openPath {
		return openTarget(args[1:])
	}
	return submitTarget(args[1:])
}

// openTarget reads `open`'s arguments for the app bundle it opens.
func openTarget(args []string) (func(string) bool, bool) {
	for i, arg := range args {
		var bundle string
		switch {
		case arg == "-a" && i+1 < len(args):
			bundle = args[i+1]
		case strings.HasSuffix(strings.TrimSuffix(arg, "/"), ".app") && !strings.HasPrefix(arg, "-"):
			bundle = arg
		default:
			continue
		}
		bundle = strings.TrimSuffix(bundle, "/")
		if strings.Contains(bundle, "/") {
			prefix := strings.ToLower(bundle) + "/"
			return func(exe string) bool { return strings.HasPrefix(strings.ToLower(exe), prefix) }, true
		}
		inside := "/" + strings.ToLower(strings.TrimSuffix(bundle, ".app")) + ".app/"
		return func(exe string) bool { return strings.Contains(strings.ToLower(exe), inside) }, true
	}
	return nil, false
}

// submitTarget reads `launchctl submit`'s arguments for the program after `--`.
func submitTarget(args []string) (func(string) bool, bool) {
	if args[0] != "submit" {
		return nil, false
	}
	for i, arg := range args {
		if arg != "--" || i+1 >= len(args) {
			continue
		}
		program := args[i+1]
		if strings.Contains(program, "/") {
			return func(exe string) bool { return exe == program }, true
		}
		return func(exe string) bool { return path.Base(exe) == program }, true
	}
	return nil, false
}

// launchLinks infers the launches the forest's launchers made: for each, the first launchd child forked within launchWindow of its
// exec whose executable matches its target, each child linked at most once. When extend is set (a story or session read), a launched
// process the forest does not hold is returned with its descendants as extra roots, so the scope shows what it caused to run, and
// truncated reports that those descendants were cut; a window read links only what it already holds. Two reads at most: every
// launcher's candidates at once, and every launched story at once.
func (q *Query) launchLinks(
	ctx context.Context, hostID string, forest []api.ProcessNode, extend bool,
) (links []api.ProcessLink, extra []api.ProcessNode, truncated bool, err error) {
	held, launchers := collectLaunchers(forest)
	if len(launchers) == 0 {
		return nil, nil, false, nil
	}
	ranges := make([]api.TimeRange, len(launchers))
	for i, l := range launchers {
		ranges[i] = api.TimeRange{FromNs: launchedFrom(l), ToNs: launchedFrom(l) + launchWindow.Nanoseconds()}
	}
	candidates, err := q.store.GetLaunchdChildren(ctx, hostID, ranges, maxLaunchCandidates)
	if err != nil {
		return nil, nil, false, err
	}
	linked := map[int64]bool{}
	var launched []api.Process
	for i, launcher := range launchers {
		match, _ := launchTarget(launcher)
		for _, c := range candidates {
			inWindow := c.ForkTimeNs >= ranges[i].FromNs && c.ForkTimeNs <= ranges[i].ToNs
			if inWindow && match(c.Path) && !linked[c.ID] && (extend || held[c.ID]) {
				linked[c.ID] = true
				links = append(links, api.ProcessLink{From: launcher.ID, To: c.ID, Kind: api.ProcessLinkInferredLaunch})
				if !held[c.ID] {
					launched = append(launched, c)
				}
				break
			}
		}
	}
	if !extend || len(launched) == 0 {
		return links, nil, false, nil
	}
	extra, truncated, err = q.launchedSubtrees(ctx, hostID, launched, held)
	return links, extra, truncated, err
}

// launchedFrom is when a launcher asked for its launch: its exec, or its fork when the row carries no exec time.
func launchedFrom(p api.Process) int64 {
	if p.ExecTimeNs != nil {
		return *p.ExecTimeNs
	}
	return p.ForkTimeNs
}

// collectLaunchers walks the forest for the rows it holds and its launchers, ordered by when each asked, so of two launches of one
// app the earlier ask claims the earlier launch and the links come out in the order the launches happened. The cap applies after
// ordering, so the oldest launchers are the ones kept.
func collectLaunchers(forest []api.ProcessNode) (map[int64]bool, []api.Process) {
	held := map[int64]bool{}
	launchers := []api.Process{}
	var walk func(nodes []api.ProcessNode)
	walk = func(nodes []api.ProcessNode) {
		for _, n := range nodes {
			held[n.ID] = true
			if _, ok := launchTarget(n.Process); ok {
				launchers = append(launchers, n.Process)
			}
			walk(n.Children)
		}
	}
	walk(forest)
	slices.SortFunc(launchers, func(a, b api.Process) int {
		return cmp.Or(cmp.Compare(launchedFrom(a), launchedFrom(b)), cmp.Compare(a.ID, b.ID))
	})
	return held, launchers[:min(len(launchers), maxLaunchers)]
}

// launchedSubtrees reads the launched processes' stories in one read and returns each launched process with its descendants,
// leaving out launchd's xpcproxy row (plumbing, not what the launcher asked for) and any row the forest already holds or an earlier
// subtree added, so no process is listed twice. truncated reports that the stories had more rows than are read.
func (q *Query) launchedSubtrees(
	ctx context.Context, hostID string, launched []api.Process, held map[int64]bool,
) ([]api.ProcessNode, bool, error) {
	var roots []int64
	for _, l := range launched {
		if l.StoryRootID != nil {
			roots = append(roots, *l.StoryRootID)
		}
	}
	procs, err := q.store.GetStoriesProcesses(ctx, hostID, roots, maxLaunchStory+1)
	if err != nil {
		return nil, false, err
	}
	truncated := len(procs) > maxLaunchStory
	if truncated {
		procs = procs[:maxLaunchStory]
	}
	stories := buildForest(procs, nil)
	var extra []api.ProcessNode
	for _, l := range launched {
		if held[l.ID] {
			continue
		}
		node := api.ProcessNode{Process: l}
		for _, root := range stories {
			if found := findInForest(root, l.ID); found != nil {
				node = *found
				break
			}
		}
		if pruned, ok := pruneHeld(node, held); ok {
			extra = append(extra, pruned)
		}
	}
	return extra, truncated, nil
}

// pruneHeld returns n without the rows held already holds, marking the rest held, and false when n itself is held.
func pruneHeld(n api.ProcessNode, held map[int64]bool) (api.ProcessNode, bool) {
	if held[n.ID] {
		return api.ProcessNode{}, false
	}
	held[n.ID] = true
	children := n.Children
	n.Children = nil
	for _, c := range children {
		if kept, ok := pruneHeld(c, held); ok {
			n.Children = append(n.Children, kept)
		}
	}
	return n, true
}

// findInForest returns the node of the row with the given id under n, n included, or nil.
func findInForest(n api.ProcessNode, id int64) *api.ProcessNode {
	if n.ID == id {
		return &n
	}
	for _, c := range n.Children {
		if found := findInForest(c, id); found != nil {
			return found
		}
	}
	return nil
}

// linkEnds is the rows aggregation must keep out of a group: the pinned process and both ends of every link, so a client can find
// each end of an edge it draws.
func linkEnds(links []api.ProcessLink, pinnedID int64) map[int64]bool {
	keep := map[int64]bool{pinnedID: pinnedID != 0}
	for _, l := range links {
		keep[l.From], keep[l.To] = true, true
	}
	return keep
}
