package sessioncommands

import (
	"cmp"
	"encoding/json"
	"slices"
	"strconv"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

// Shaping an AI agent session's process forest into the commands it ran (issue #1284). The forest is built by creator. The agent
// process itself carries no session marker, so each command's shell is a root, and everything that command started (a pipeline's
// commands, a daemon it double-forked or nohup'ed) hangs under it.

// node is a forest node that can be moved by pointer while links are applied.
type node struct {
	process  api.ProcessNode
	children []*node
}

func toNodes(forest []api.ProcessNode) []*node {
	out := make([]*node, 0, len(forest))
	for _, p := range forest {
		n := &node{process: p, children: toNodes(p.Children)}
		n.process.Children = nil
		out = append(out, n)
	}
	return out
}

func fromNodes(nodes []*node) []api.ProcessNode {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]api.ProcessNode, 0, len(nodes))
	for _, n := range nodes {
		p := n.process
		p.Children = fromNodes(n.children)
		out = append(out, p)
	}
	return out
}

// located is a node and the slice that holds it.
type located struct {
	node     *node
	siblings *[]*node
}

// locate returns the node with the given row id and the slice that holds it, or nil when the forest does not hold it.
func locate(nodes *[]*node, id int64) *located {
	for _, n := range *nodes {
		if n.process.ID == id {
			return &located{node: n, siblings: nodes}
		}
		if found := locate(&n.children, id); found != nil {
			return found
		}
	}
	return nil
}

func holds(n *node, id int64) bool {
	return n.process.ID == id || slices.ContainsFunc(n.children, func(c *node) bool { return holds(c, id) })
}

func byForkTime(a, b api.ProcessNode) int {
	return cmp.Or(cmp.Compare(a.ForkTimeNs, b.ForkTimeNs), cmp.Compare(a.ID, b.ID))
}

// LogicalForest is a forest with what launchd started moved under the process that asked for it (issue #1286).
type LogicalForest struct {
	Roots []api.ProcessNode
	// Inferred names the rows an inferred launch moved under a launcher, drawn as an inferred edge rather than a parent edge.
	Inferred map[int64]bool
	// Services names the rows moved under the app responsible for them (issue #1340): a link the OS recorded, not inferred.
	Services map[int64]bool
}

// ApplyLaunchLinks returns the forest with each link's launched process moved under its launcher, or a service under the app it
// serves, in fork order among that process's children. A link whose ends are not both in the forest, or whose moved process holds
// the other end, is left as it is. The caller's forest is not changed.
func ApplyLaunchLinks(roots []api.ProcessNode, links []api.ProcessLink) LogicalForest {
	out := LogicalForest{Inferred: map[int64]bool{}, Services: map[int64]bool{}}
	forest := toNodes(roots)
	for _, link := range links {
		launched, launcher := locate(&forest, link.To), locate(&forest, link.From)
		if launched == nil || launcher == nil || holds(launched.node, link.From) {
			continue
		}
		*launched.siblings = slices.DeleteFunc(*launched.siblings, func(n *node) bool { return n == launched.node })
		launcher.node.children = append(launcher.node.children, launched.node)
		slices.SortStableFunc(launcher.node.children, func(a, b *node) int { return byForkTime(a.process, b.process) })
		if link.Kind == api.ProcessLinkResponsibleProcess {
			out.Services[link.To] = true
		} else {
			out.Inferred[link.To] = true
		}
	}
	out.Roots = fromNodes(forest)
	if out.Roots == nil {
		out.Roots = []api.ProcessNode{}
	}
	return out
}

// Command is one command an agent session ran: the processes a single command started, as a forest under its root.
type Command struct {
	// Key identifies the command by the row of the root that started it, RootID.
	Key    string `json:"key"`
	RootID int64  `json:"-"`
	// Command is what the agent ran: the wrapper's eval argument, or the root's own command line when it is not such a wrapper.
	Command string `json:"command"`
	// FromAgent is set when the command's root is the agent's wrapper shell, so its command is one the agent ran as a Bash call.
	FromAgent bool `json:"from_agent"`
	// UID is the user the command's root ran as.
	UID     *int  `json:"uid,omitempty"`
	StartNs int64 `json:"start_ns"`
	// LastNs is the command's last observed activity: its latest observed exit, exec or fork.
	LastNs int64 `json:"last_ns"`
	// ProcessIDs are the rows of every process the command started, shell plumbing included, in the order its forest lists them.
	ProcessIDs []int64 `json:"process_ids"`
	// Roots are the command's processes as a forest: its root, and any root that joined it because its creator was not read.
	Roots []api.ProcessNode `json:"roots"`
	// ToolCall is the tool call the agent reported for the command, when one pairs with it.
	ToolCall *ToolCallMatch `json:"tool_call,omitempty"`
}

// args decodes a process's argv. A process with no argv, or argv the server could not read as a list of strings, has none.
func args(p api.ProcessNode) []string {
	var out []string
	if len(p.Args) == 0 || json.Unmarshal(p.Args, &out) != nil {
		return nil
	}
	return out
}

// lastActivityNs is when a process was last seen doing something. Only an observed exit counts: the server synthesizes the others
// (a stale process closed by its sweep hours later, a pid reused), and their time says when the server noticed, not when the process
// ran. A re-exec's exit is the moment the next image started.
func lastActivityNs(p api.ProcessNode) int64 {
	if p.ExitReason != nil && p.ExitTimeNs != nil && (*p.ExitReason == api.ExitReasonEvent || *p.ExitReason == api.ExitReasonReExec) {
		return *p.ExitTimeNs
	}
	if p.ExecTimeNs != nil {
		return *p.ExecTimeNs
	}
	return p.ForkTimeNs
}

// walk visits a process and everything under it.
func walk(p api.ProcessNode, visit func(api.ProcessNode)) {
	visit(p)
	for _, c := range p.Children {
		walk(c, visit)
	}
}

// Build groups the session's forest by command, in the order the commands started. Each command's leader (the shell that leads the
// process group keying it) starts a command. A root whose creator was not read joins the latest command its key names that started
// before it, so a command stays one even when part of its tree arrived without its parent. The key is a process-group id, which a
// long session can reuse, so a later leader with the same key starts a command of its own rather than joining the earlier one.
func Build(roots []api.ProcessNode) []Command {
	sorted := slices.Clone(roots)
	slices.SortStableFunc(sorted, byForkTime)
	commands := []Command{}
	latestByKey := map[int]int{}
	for _, root := range sorted {
		key := root.ToolCallGroup
		at, joined := -1, false
		if key != nil && root.PID != *key {
			at, joined = latestByKey[*key]
		}
		if !joined {
			argv := args(root)
			command, fromAgent := ToolCommand(argv)
			if !fromAgent {
				command = CommandLine(root.Path, argv)
			}
			commands = append(commands, Command{
				Key: "row:" + strconv.FormatInt(root.ID, 10), RootID: root.ID, Command: command, FromAgent: fromAgent, UID: root.UID,
				StartNs: root.ForkTimeNs, LastNs: root.ForkTimeNs, ProcessIDs: []int64{}, Roots: []api.ProcessNode{},
			})
			at = len(commands) - 1
			if key != nil {
				latestByKey[*key] = at
			}
		}
		into := &commands[at]
		walk(root, func(p api.ProcessNode) {
			into.ProcessIDs = append(into.ProcessIDs, p.ID)
			into.LastNs = max(into.LastNs, lastActivityNs(p))
		})
		into.Roots = append(into.Roots, root)
	}
	return commands
}
