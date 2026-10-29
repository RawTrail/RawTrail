import { describe, expect, it } from "vitest";
import type { DomainActivity } from "../../types";
import { barHeights, directoryLabel, fileOperations, groupDomains } from "./sessionDigest";

function domain(name: string, lookups = 1): DomainActivity {
  return { domain: name, lookups, connections: 0, new: false };
}

describe("groupDomains", () => {
  it("groups domains by organization in a fixed order, everything else last, each group busiest first", () => {
    const groups = groupDomains([domain("example.com", 9), domain("github.com", 5), domain("npmjs.org", 4), domain("anthropic.com", 3),
      domain("githubusercontent.com", 2), domain("internal.test", 1)]);
    expect(groups.map((g) => [g.label, g.domains.map((d) => d.domain)])).toEqual([
      ["Model APIs", ["anthropic.com"]],
      ["Git hosts", ["github.com", "githubusercontent.com"]],
      ["Package registries", ["npmjs.org"]],
      ["Everything else", ["example.com", "internal.test"]],
    ]);
  });

  it("puts a name under a known domain with it", () => {
    const groups = groupDomains([domain("objects.githubusercontent.com"), domain("notgithub.com")]);
    expect(groups.map((g) => [g.label, g.domains.map((d) => d.domain)])).toEqual([["Git hosts", ["objects.githubusercontent.com"]], ["Everything else", ["notgithub.com"]]]);
  });

  it("leaves out an organization no domain belongs to", () => {
    expect(groupDomains([domain("pypi.org")]).map((g) => g.label)).toEqual(["Package registries"]);
    expect(groupDomains([])).toEqual([]);
  });
});

describe("directoryLabel", () => {
  it.each([
    ["the project itself", "/Users/dev/app", "/Users/dev/app", "."],
    ["a directory under the project", "/Users/dev/app/src/lib", "/Users/dev/app", "./src/lib"],
    ["a project named with a trailing slash", "/Users/dev/app/src", "/Users/dev/app/", "./src"],
    ["a sibling sharing the project's prefix", "/Users/dev/app-old", "/Users/dev/app", "/Users/dev/app-old"],
    ["a directory outside the project", "/etc", "/Users/dev/app", "/etc"],
    ["no project", "/Users/dev/app/src", undefined, "/Users/dev/app/src"],
  ])("names %s", (_, directory, project, want) => {
    expect(directoryLabel(directory, project)).toBe(want);
  });
});

describe("fileOperations", () => {
  it("counts an agent's operation with the watched-path event that means the same, in a fixed order, leaving out none", () => {
    expect(fileOperations({ file_delete: 1, modify: 5, create: 2, file_truncate: 1, delete: 2, open: 3, rename: 0 })).toEqual([
      { label: "created", count: 2 },
      { label: "modified", count: 6 },
      { label: "deleted", count: 3 },
      { label: "opened", count: 3 },
    ]);
  });
});

describe("barHeights", () => {
  it("scales each bucket to the busiest, keeping a small one visible and an empty one empty", () => {
    expect(barHeights([0, 1, 200, 100])).toEqual([0, 6, 100, 50]);
  });

  it("draws no bars for a timeline with nothing on it", () => {
    expect(barHeights([0, 0])).toEqual([]);
    expect(barHeights([])).toEqual([]);
  });
});
