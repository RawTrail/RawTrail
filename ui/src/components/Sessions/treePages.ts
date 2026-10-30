// Joining the pages of a session's process tree (issue #1424). The server reads a session oldest first and pages it by cursor, so a
// process's creator is read before it: on its own page, where the page nests it, or on an earlier one, which this joins it to.

import type { ProcessNode, TreeResponse } from "../../types";

function copyForest(nodes: readonly ProcessNode[]): ProcessNode[] {
  return nodes.map((n) => ({ ...n, children: n.children ? copyForest(n.children) : undefined }));
}

function index(nodes: readonly ProcessNode[], into: Map<number, ProcessNode>): void {
  for (const n of nodes) {
    into.set(n.id, n);
    index(n.children ?? [], into);
  }
}

const forkOrder = (a: ProcessNode, b: ProcessNode) => a.fork_time_ns - b.fork_time_ns || a.id - b.id;

// mergeTreePages returns the tree of two pages read in turn: the next page's roots whose creator the earlier page read hang under
// it, in fork order, and the rest stay roots. Its links join the earlier page's; its counts and cursor are the next page's, the last
// read. A cut of what a page's links added stays, since no later page reads it again. Neither page is changed.
export function mergeTreePages(earlier: TreeResponse, next: TreeResponse): TreeResponse {
  const roots = copyForest(earlier.roots);
  const byId = new Map<number, ProcessNode>();
  index(roots, byId);
  // Each creator's children are appended to, then sorted once: sorting per arrival is quadratic in a wide page.
  const grown = new Set<ProcessNode>();
  for (const root of copyForest(next.roots)) {
    const creator = root.parent_process_id === undefined ? undefined : byId.get(root.parent_process_id);
    if (creator) {
      (creator.children ??= []).push(root);
      grown.add(creator);
    } else {
      roots.push(root);
    }
    index([root], byId);
  }
  for (const creator of grown) creator.children?.sort(forkOrder);
  const linksTruncated = earlier.links_truncated === true || next.links_truncated === true;
  const links = [...(earlier.links ?? []), ...(next.links ?? [])];
  return {
    ...earlier,
    roots,
    links: links.length > 0 ? links : undefined,
    returned: earlier.returned + next.returned,
    total_matched: next.total_matched,
    total_matched_capped: next.total_matched_capped,
    truncated: next.truncated || linksTruncated,
    links_truncated: linksTruncated || undefined,
    next_cursor: next.next_cursor,
  };
}
