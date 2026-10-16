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
	// exactLaunchWindow is the window for a match on a job label or bundle id, which names one job or app exactly, so it can reach far
	// enough for launchd's restart throttle: `launchctl kickstart -k` restarted its job 5.0 s after the command on edr-dev (#1341).
	exactLaunchWindow = 15 * time.Second
	// maxLaunchers bounds how many launchers one read infers for, the oldest kept.
	maxLaunchers = 50
	// maxLaunchCandidates bounds the launchd children read for each launcher's window, so one busy window cannot starve another.
	maxLaunchCandidates = 200
	// maxLaunchStory bounds the rows a scoped read adds for the programs its launchers started; past it the read reports truncation.
	maxLaunchStory = 2000
	// The launchers, by their canonical paths: a binary elsewhere that happens to be named open or launchctl is not Apple's.
	openPath      = "/usr/bin/open"
	launchctlPath = "/bin/launchctl"
	// xpcproxyPath is launchd's spawn trampoline: each job starts as an xpcproxy row naming the job's label, re-exec'd into the program.
	xpcproxyPath = "/usr/libexec/xpcproxy"
)

// launchMatch is what a launcher asked launchd to start: a test over the candidate launchd children, and how long after the ask one
// may start.
type launchMatch struct {
	match  func(api.Process) bool
	window time.Duration
}

// byPath matches a candidate whose executable path the test accepts, within launchWindow.
func byPath(accept func(string) bool) launchMatch {
	return launchMatch{match: func(c api.Process) bool { return accept(c.Path) }, window: launchWindow}
}

// launchTarget returns what a launcher exec asked launchd to start, and false for an exec that is no launcher or names nothing to
// match:
//
//   - `open -a App` or `open -a /path/App.app`, and `open /path/App.app`: the executable lies inside that bundle.
//   - `open -b bundle.id`: the executable's code-signing identifier is the bundle id (#1341).
//   - `launchctl submit ... -- program [args]`: the executable is the program.
//   - `launchctl kickstart <domain>/<label>` and `launchctl start <label>`: launchd's xpcproxy row for the job names the label (#1341).
//
// `launchctl load|bootstrap plist` names a file, not the label inside it, and is not inferred.
func launchTarget(p api.Process) (launchMatch, bool) {
	if p.Path != openPath && p.Path != launchctlPath {
		return launchMatch{}, false
	}
	// The argv is stored as the JSON array the exec reported; one that does not decode names nothing to match.
	var args []string
	if json.Unmarshal(p.Args, &args) != nil || len(args) < 2 {
		return launchMatch{}, false
	}
	if p.Path == openPath {
		return openTarget(args[1:])
	}
	if label, ok := jobLabel(args[1:]); ok {
		return launchMatch{match: func(c api.Process) bool { return xpcproxyLabel(c) == label }, window: exactLaunchWindow}, true
	}
	return submitTarget(args[1:])
}

// jobLabel reads the label `launchctl kickstart [flags] <domain>/<label>` or `launchctl start <label>` names.
func jobLabel(args []string) (string, bool) {
	switch args[0] {
	case "start":
		if len(args) == 2 {
			return args[1], true
		}
	case "kickstart":
		target := args[len(args)-1]
		if i := strings.LastIndex(target, "/"); i >= 0 && i+1 < len(target) && len(args) >= 2 && !strings.HasPrefix(target, "-") {
			return target[i+1:], true
		}
	}
	return "", false
}

// xpcproxyLabel is the job label an xpcproxy row names, and "" for any other row.
func xpcproxyLabel(c api.Process) string {
	var args []string
	if c.Path != xpcproxyPath || json.Unmarshal(c.Args, &args) != nil || len(args) < 2 {
		return ""
	}
	return args[1]
}

// signingID is a row's code-signing identifier, and "" when it has none.
func signingID(c api.Process) string {
	var cs struct {
		SigningID string `json:"signing_id"`
	}
	if json.Unmarshal(c.CodeSigning, &cs) != nil {
		return ""
	}
	return cs.SigningID
}

// openTarget reads `open`'s arguments for the app it opens, by bundle path or name, or by bundle id.
func openTarget(args []string) (launchMatch, bool) {
	for i, arg := range args {
		var bundle string
		switch {
		case arg == "-b" && i+1 < len(args):
			id := args[i+1]
			return launchMatch{match: func(c api.Process) bool { return strings.EqualFold(signingID(c), id) }, window: exactLaunchWindow}, true
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
			return byPath(func(exe string) bool { return strings.HasPrefix(strings.ToLower(exe), prefix) }), true
		}
		inside := "/" + strings.ToLower(strings.TrimSuffix(bundle, ".app")) + ".app/"
		return byPath(func(exe string) bool { return strings.Contains(strings.ToLower(exe), inside) }), true
	}
	return launchMatch{}, false
}

// submitTarget reads `launchctl submit`'s arguments for the program after `--`.
func submitTarget(args []string) (launchMatch, bool) {
	if args[0] != "submit" {
		return launchMatch{}, false
	}
	for i, arg := range args {
		if arg != "--" || i+1 >= len(args) {
			continue
		}
		program := args[i+1]
		if strings.Contains(program, "/") {
			return byPath(func(exe string) bool { return exe == program }), true
		}
		return byPath(func(exe string) bool { return path.Base(exe) == program }), true
	}
	return launchMatch{}, false
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
	targets := make([]launchMatch, len(launchers))
	ranges := make([]api.TimeRange, len(launchers))
	for i, l := range launchers {
		targets[i], _ = launchTarget(l)
		ranges[i] = api.TimeRange{FromNs: launchedFrom(l), ToNs: launchedFrom(l) + targets[i].window.Nanoseconds()}
	}
	candidates, err := q.store.GetLaunchdChildren(ctx, hostID, ranges, maxLaunchCandidates)
	if err != nil {
		return nil, nil, false, err
	}
	linked := map[int64]bool{}
	var launched []api.Process
	for i, launcher := range launchers {
		for _, c := range candidates {
			inWindow := c.ForkTimeNs >= ranges[i].FromNs && c.ForkTimeNs <= ranges[i].ToNs
			if !inWindow || !targets[i].match(c) {
				continue
			}
			// A label names launchd's xpcproxy row for the job; what the launcher asked for is the program it re-exec'd into.
			program, ok := launchedProgram(candidates, c)
			if !ok || linked[program.ID] || (!extend && !held[program.ID]) {
				continue
			}
			linked[program.ID] = true
			links = append(links, api.ProcessLink{From: launcher.ID, To: program.ID, Kind: api.ProcessLinkInferredLaunch})
			if !held[program.ID] {
				launched = append(launched, program)
			}
			break
		}
	}
	if !extend || len(launched) == 0 {
		return links, nil, false, nil
	}
	extra, truncated, err = q.launchedSubtrees(ctx, hostID, launched, held)
	return links, extra, truncated, err
}

// launchedProgram is the program a matched launchd child stands for: the child itself, or, for launchd's xpcproxy row, the program
// that re-exec'd the same pid from it. An xpcproxy row the window holds no program for stands for nothing yet.
func launchedProgram(candidates []api.Process, c api.Process) (api.Process, bool) {
	if c.Path != xpcproxyPath {
		return c, true
	}
	for _, p := range candidates {
		if p.PreviousExecID != nil && *p.PreviousExecID == c.ID {
			return p, true
		}
	}
	return api.Process{}, false
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
