import { describe, it, expect } from "vitest";

import { applyLaunchLinks } from "./launchLinks";
import type { ProcessNode } from "../types";

function node(id: number, fork: number, children: ProcessNode[] = []): ProcessNode {
  return { id, host_id: "H1", pid: id, ppid: 0, path: `/bin/p${String(id)}`, fork_time_ns: fork, children };
}

// session is the edr-dev capture's shape: a tool call's shell (1) ran open (2), and launchd started TextEdit (3) a root of its own,
// carrying a child (4); another call's shell (5) ran launchctl (6), and launchd started sleep (7).
function session(): ProcessNode[] {
  return [node(1, 10, [node(2, 11, [node(9, 12)])]), node(3, 15, [node(4, 16)]), node(5, 30, [node(6, 31)]), node(7, 32)];
}

function outline(nodes: ProcessNode[], depth = 0): string[] {
  return nodes.flatMap((n) => [`${"  ".repeat(depth)}${String(n.id)}`, ...outline(n.children ?? [], depth + 1)]);
}

describe("applyLaunchLinks", () => {
  // spec:web-ui/inferred-launches-in-the-ui/a-launched-process-hangs-under-its-launcher
  it("moves each launched process, with what it started, under its launcher in fork order", () => {
    const { roots, inferred } = applyLaunchLinks(session(), [
      { from: 2, to: 3, kind: "inferred_launch" },
      { from: 6, to: 7, kind: "inferred_launch" },
    ]);
    expect(outline(roots)).toEqual(["1", "  2", "    9", "    3", "      4", "5", "  6", "    7"]);
    expect([...inferred]).toEqual([3, 7]);
  });

  // spec:web-ui/services-under-their-app/a-service-hangs-under-the-app-responsible-for-it
  it("moves a service under the app responsible for it, marked as a service rather than an inferred launch", () => {
    // TextEdit (3) is in the read; launchd started its XPC service (8), which names TextEdit as responsible.
    const { roots, inferred, services } = applyLaunchLinks([...session(), node(8, 17)], [
      { from: 2, to: 3, kind: "inferred_launch" },
      { from: 3, to: 8, kind: "responsible_process" },
    ]);
    expect(outline(roots)).toEqual(["1", "  2", "    9", "    3", "      4", "      8", "5", "  6", "7"]);
    expect([...inferred]).toEqual([3]);
    expect([...services]).toEqual([8]);
  });

  it("leaves the caller's forest as it was", () => {
    const original = session();
    applyLaunchLinks(original, [{ from: 2, to: 3, kind: "inferred_launch" }]);
    expect(outline(original)).toEqual(outline(session()));
  });

  it.each([
    ["a launched process the read does not hold", { from: 2, to: 99, kind: "inferred_launch" as const }],
    ["a launcher the read does not hold", { from: 99, to: 3, kind: "inferred_launch" as const }],
    // Moving a node under its own descendant would detach both from the forest.
    ["a launched process that holds its launcher", { from: 4, to: 3, kind: "inferred_launch" as const }],
  ])("leaves %s where it is", (_name, link) => {
    const { roots, inferred } = applyLaunchLinks(session(), [link]);
    expect(outline(roots)).toEqual(outline(session()));
    expect(inferred.size).toBe(0);
  });

  it("returns the forest unchanged when there are no links", () => {
    expect(outline(applyLaunchLinks(session(), undefined).roots)).toEqual(outline(session()));
    expect(applyLaunchLinks(session(), []).inferred.size).toBe(0);
  });
});
