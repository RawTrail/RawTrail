import { describe, it, expect } from "vitest";

import { SINCE_CHOICES, searchFilter } from "./processSearchFilter";

const NOW_MS = Date.parse("2026-10-14T12:00:00Z");
const NS_PER_MS = 1_000_000;

describe("searchFilter", () => {
  it("turns each since choice into a window ending now", () => {
    for (const choice of SINCE_CHOICES) {
      expect(searchFilter({ since: choice.value, path: "curl" }, NOW_MS)).toEqual({
        path: "curl", from: String((NOW_MS - choice.reachMs) * NS_PER_MS), to: String(NOW_MS * NS_PER_MS),
      });
    }
    expect(SINCE_CHOICES.find((c) => c.value === "7d")?.reachMs).toBe(7 * 24 * 3_600_000);
  });

  it("passes every other chip through, and drops a since it does not know", () => {
    expect(searchFilter({ agent: "claude-code", args: "x" }, NOW_MS)).toEqual({ agent: "claude-code", args: "x" });
    expect(searchFilter({ since: "1y", agent: "claude-code" }, NOW_MS)).toEqual({ agent: "claude-code" });
  });
});
