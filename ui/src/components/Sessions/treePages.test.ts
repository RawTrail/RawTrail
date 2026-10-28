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
