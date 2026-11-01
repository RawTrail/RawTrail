import { describe, expect, it } from "vitest";
import type { DomainActivity } from "../../types";
import { groupDomains } from "./sessionDigest";

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
