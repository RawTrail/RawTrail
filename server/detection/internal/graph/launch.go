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
	// maxLaunchers bounds how many launchers one read infers for, each costing one indexed candidate read.
	maxLaunchers = 50
	// maxLaunchCandidates bounds the launchd children read per launcher.
	maxLaunchCandidates = 200
	// maxLaunchStory bounds the rows a scoped read adds for one launched program's story.
	maxLaunchStory = 500
)

// launchTarget returns what a launcher exec asked launchd to start, as a match over a candidate's executable path, and false for an
// exec that is no launcher or names nothing a path can match:
//
//   - `open -a App` or `open -a /path/App.app`, and `open /path/App.app`: the executable lies inside that bundle.
//   - `launchctl submit ... -- program [args]`: the executable is the program.
//
// `open -b bundle.id`, `launchctl load|bootstrap plist` and XPC hops name nothing a path matches and are not inferred.
func launchTarget(p api.Process) (func(string) bool, bool) {
	base := path.Base(p.Path)
	if base != "open" && base != "launchctl" {
		return nil, false
	}
	// The argv is stored as the JSON array the exec reported; one that does not decode names nothing to match.
	var args []string
	if json.Unmarshal(p.Args, &args) != nil || len(args) < 2 {
		return nil, false
	}
	if base == "open" {
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
// process the forest does not hold is returned with its descendants as extra roots, so the scope shows what it caused to run; a window
// read links only what it already holds.
func (q *Query) launchLinks(
	ctx context.Context, hostID string, forest []api.ProcessNode, extend bool,
) ([]api.ProcessLink, []api.ProcessNode, error) {
	held, launchers := collectLaunchers(forest)
	var links []api.ProcessLink
	var extra []api.ProcessNode
	linked := map[int64]bool{}
	for _, launcher := range launchers {
		launched, err := q.firstLaunch(ctx, hostID, launcher, func(c api.Process) bool { return !linked[c.ID] && (extend || held[c.ID]) })
		if err != nil {
			return nil, nil, err
		}
		if launched == nil {
			continue
		}
		linked[launched.ID] = true
		links = append(links, api.ProcessLink{From: launcher.ID, To: launched.ID, Kind: api.ProcessLinkInferredLaunch})
		if extend && !held[launched.ID] {
			subtree, err := q.launchedSubtree(ctx, hostID, *launched)
			if err != nil {
				return nil, nil, err
			}
			extra = append(extra, subtree...)
		}
	}
	return links, extra, nil
}

// collectLaunchers walks the forest for the rows it holds and its launchers, oldest launcher first, so of two launches of one app the
// earlier launcher claims the earlier launch and the links come out in the order the launches happened rather than the forest's.
func collectLaunchers(forest []api.ProcessNode) (map[int64]bool, []api.Process) {
	held := map[int64]bool{}
	var launchers []api.Process
	var walk func(nodes []api.ProcessNode)
	walk = func(nodes []api.ProcessNode) {
		for _, n := range nodes {
			held[n.ID] = true
			if _, ok := launchTarget(n.Process); ok && len(launchers) < maxLaunchers {
				launchers = append(launchers, n.Process)
			}
			walk(n.Children)
		}
	}
	walk(forest)
	slices.SortFunc(launchers, func(a, b api.Process) int {
		return cmp.Or(cmp.Compare(a.ForkTimeNs, b.ForkTimeNs), cmp.Compare(a.ID, b.ID))
	})
	return held, launchers
}

// firstLaunch returns the first launchd child forked within launchWindow of the launcher's exec that runs its target and that
// eligible admits, or nil when there is none.
func (q *Query) firstLaunch(
	ctx context.Context, hostID string, launcher api.Process, eligible func(api.Process) bool,
) (*api.Process, error) {
	match, _ := launchTarget(launcher)
	from := launcher.ForkTimeNs
	if launcher.ExecTimeNs != nil {
		from = *launcher.ExecTimeNs
	}
	candidates, err := q.store.GetLaunchdChildren(ctx, hostID, from, from+launchWindow.Nanoseconds(), maxLaunchCandidates)
	if err != nil {
		return nil, err
	}
	for i := range candidates {
		if match(candidates[i].Path) && eligible(candidates[i]) {
			return &candidates[i], nil
		}
	}
	return nil, nil
}

// launchedSubtree reads a launched process with its descendants from its story, leaving out the xpcproxy row it was re-exec'd from:
// that is launchd's plumbing, not what the launcher asked for.
func (q *Query) launchedSubtree(ctx context.Context, hostID string, launched api.Process) ([]api.ProcessNode, error) {
	if launched.StoryRootID == nil {
		return []api.ProcessNode{{Process: launched}}, nil
	}
	procs, err := q.store.GetScopedProcesses(ctx, hostID, api.TreeScope{StoryRootID: *launched.StoryRootID}, maxLaunchStory)
	if err != nil {
		return nil, err
	}
	for _, root := range buildForest(procs, nil) {
		if node := findInForest(root, launched.ID); node != nil {
			return []api.ProcessNode{*node}, nil
		}
	}
	return []api.ProcessNode{{Process: launched}}, nil
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
