import { describe, it, expect } from "vitest";

import { mergeTreePages } from "./treePages";
import { proc } from "../../test/agentSession";
import type { TreeResponse } from "../../types";

function page(roots: TreeResponse["roots"], over: Partial<TreeResponse> = {}): TreeResponse {
  return { roots, returned: 1, total_matched: 3, total_matched_capped: false, truncated: true, next_cursor: "c", ...over };
}

describe("mergeTreePages", () => {
  // spec:web-ui/agent-session-view-pages-its-processes-and-activity/the-rest-of-the-processes-come-on-request
  it("hangs a later page's process under its creator from an earlier page, and keeps the rest as roots", () => {
    const shell = proc(1, 100, 10, "/bin/zsh", ["zsh"], [proc(2, 101, 11, "/bin/ls", ["ls"])]);
    const first = page([shell], { links: [{ from: 1, to: 2, kind: "inferred_launch" }] });
    // The shell's late child, the ls's child, and a root whose creator no page read.
    const late = proc(3, 102, 30, "/usr/bin/git", ["git"], [], { parent_process_id: 1 });
    const grandchild = proc(4, 103, 31, "/usr/bin/less", ["less"], [], { parent_process_id: 2 });
    const stray = proc(5, 104, 32, "/bin/sh", ["sh"], [], { parent_process_id: 99 });
    const early = proc(6, 105, 9, "/usr/bin/env", ["env"], [], { parent_process_id: 1 });
    const second = page([late, grandchild, stray, early], { returned: 4, total_matched: 6, truncated: false, next_cursor: undefined,
      links: [{ from: 3, to: 4, kind: "inferred_launch" }] });

    const merged = mergeTreePages(first, second);
    expect(merged.roots.map((r) => r.id)).toEqual([1, 5]);
    expect(merged.roots[0].children?.map((c) => c.id)).toEqual([6, 2, 3]);
    expect(merged.roots[0].children?.[1].children?.map((c) => c.id)).toEqual([4]);
    expect(merged.links).toHaveLength(2);
    expect(merged).toMatchObject({ returned: 5, total_matched: 6, truncated: false, next_cursor: undefined });
    expect(first.roots[0].children?.map((c) => c.id)).toEqual([2]);
    expect(second.roots).toHaveLength(4);
  });

  // spec:server-rest-api/a-tree-read-reports-its-links-cut/a-launched-program-cut-is-told-apart-from-paging
  it("keeps a page's cut of what its links added, which no later page reads again", () => {
    const shell = proc(1, 100, 10, "/bin/zsh", ["zsh"]);
    const merged = mergeTreePages(page([shell], { links_truncated: true }),
      page([proc(2, 101, 11, "/bin/ls", ["ls"])], { truncated: false, next_cursor: undefined }));
    expect(merged).toMatchObject({ truncated: true, links_truncated: true, next_cursor: undefined });
    expect(mergeTreePages(page([shell]), page([], { truncated: false, next_cursor: undefined })))
      .toMatchObject({ truncated: false, links_truncated: undefined });
  });

  it("lists a creator's children in fork order when a page brings many of them out of order", () => {
    const shell = proc(1, 100, 10, "/bin/zsh", ["zsh"], [proc(2, 101, 50, "/bin/ls", ["ls"])]);
    const late = [40, 60, 20, 55].map((fork, i) => proc(10 + i, 200 + i, fork, "/bin/echo", ["echo"], [], { parent_process_id: 1 }));
    const merged = mergeTreePages(page([shell]), page(late));
    // By fork time: 20 (id 12), 40 (id 10), 50 (id 2, the earlier page's), 55 (id 13), 60 (id 11).
    expect(merged.roots[0].children?.map((c) => c.id)).toEqual([12, 10, 2, 13, 11]);
  });

  it("hangs a process under a creator its own page also read later", () => {
    const shell = proc(1, 100, 10, "/bin/zsh", ["zsh"]);
    const child = proc(2, 101, 11, "/bin/ls", ["ls"], [], { parent_process_id: 1 });
    const grandchild = proc(3, 102, 12, "/usr/bin/less", ["less"], [], { parent_process_id: 2 });
    const merged = mergeTreePages(page([shell]), page([child, grandchild], { links: undefined }));
    expect(merged.roots.map((r) => r.id)).toEqual([1]);
    expect(merged.roots[0].children?.[0].children?.map((c) => c.id)).toEqual([3]);
    expect(merged.links).toBeUndefined();
  });
});
