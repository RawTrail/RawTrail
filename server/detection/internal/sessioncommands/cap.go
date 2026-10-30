package sessioncommands

import "github.com/rawtrail/rawtrail/server/detection/api"

// InlineProcesses is how many of a command's processes a page of commands lists inline (issue #1443). The view shows a command's
// first rows before "Show more", so a command that ran a build or a test run with a worker per case is listed in part and read whole
// on request, which keeps a page's size bounded by its limit.
const InlineProcesses = 100

// Cap lists a command's processes in part when it holds more than limit: the first limit in its forest's order, and every process
// keep holds, each with the processes above it so the forest keeps its shape. ProcessIDs stay whole, since what is not listed still
// counts toward the command's alerts and size. A command that so leaves any process out is marked ProcessesCut.
func Cap(c Command, limit int, keep func(api.ProcessNode) bool) Command {
	if len(c.ProcessIDs) <= limit {
		return c
	}
	listed, omitted := 0, false
	var prune func(p api.ProcessNode) (api.ProcessNode, bool)
	prune = func(p api.ProcessNode) (api.ProcessNode, bool) {
		inFirst := listed < limit
		listed++
		var children []api.ProcessNode
		for _, child := range p.Children {
			if kept, ok := prune(child); ok {
				children = append(children, kept)
			}
		}
		if !inFirst && !keep(p) && len(children) == 0 {
			omitted = true
			return api.ProcessNode{}, false
		}
		p.Children = children
		return p, true
	}
	roots := []api.ProcessNode{}
	for _, root := range c.Roots {
		if kept, ok := prune(root); ok {
			roots = append(roots, kept)
		}
	}
	c.Roots = roots
	c.ProcessesCut = omitted
	return c
}

// Matching is the keep set of a search: the processes Search finds the query in, so a hit is listed however far into its command
// it lies. An empty query keeps nothing.
func Matching(c Command, rawQuery string) func(api.ProcessNode) bool {
	query := normalize(rawQuery)
	return func(p api.ProcessNode) bool {
		return query != "" && matchesProcess(p, query, pathOnly(c, p))
	}
}
