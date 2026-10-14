// Re-hanging what launchd started under the process that asked for it (issue #1286). The server links an open or launchctl exec to
// the launchd child that ran what it named; the forest keeps that child where its parentage puts it, a root under launchd. A logical
// view moves it, and everything it started, under its launcher instead, and remembers which rows it moved so they can be drawn as
// inferred rather than as lineage.

import type { ProcessLink, ProcessNode } from "../types";

export interface LogicalForest {
  roots: ProcessNode[];
  // inferred names the rows moved under a launcher, which a view draws as an inferred edge rather than a parent edge.
  inferred: Set<number>;
}

// copyForest returns the forest with fresh node objects and child arrays, so moving a node does not change the caller's forest.
function copyForest(nodes: readonly ProcessNode[]): ProcessNode[] {
  return nodes.map((n) => ({ ...n, children: n.children ? copyForest(n.children) : undefined }));
}

// locate returns the node with the given id and the array that holds it.
function locate(nodes: ProcessNode[], id: number): { node: ProcessNode; siblings: ProcessNode[] } | null {
  for (const n of nodes) {
    if (n.id === id) return { node: n, siblings: nodes };
    const found = n.children ? locate(n.children, id) : null;
    if (found) return found;
  }
  return null;
}

function holds(node: ProcessNode, id: number): boolean {
  return node.id === id || (node.children ?? []).some((c) => holds(c, id));
}

// applyLaunchLinks returns the forest with each link's launched process moved under its launcher, in fork order among the launcher's
// children. A link whose ends are not both in the forest, or whose launched process holds its launcher, is left as it is.
export function applyLaunchLinks(roots: readonly ProcessNode[], links: readonly ProcessLink[] | undefined): LogicalForest {
  const inferred = new Set<number>();
  if (!links || links.length === 0) return { roots: [...roots], inferred };
  const forest = copyForest(roots);
  for (const link of links) {
    const launched = locate(forest, link.to);
    const launcher = locate(forest, link.from);
    if (!launched || !launcher || holds(launched.node, link.from)) continue;
    launched.siblings.splice(launched.siblings.indexOf(launched.node), 1);
    launcher.node.children = [...(launcher.node.children ?? []), launched.node].sort(
      (a, b) => a.fork_time_ns - b.fork_time_ns || a.id - b.id,
    );
    inferred.add(link.to);
  }
  return { roots: forest, inferred };
}
