import { describe, expect, it } from "vitest";
import { scopeNote } from "./activityScope";

describe("scopeNote", () => {
  it.each([
    [{ applied: true, processes: 9, unmatched: 2, truncated: true },
      "This scope has more processes than are read, so its activity is partial."],
    [{ applied: false, processes: 0, unmatched: 0, truncated: false },
      "No stored process matches this scope, so there is no activity to show."],
    [{ applied: false, processes: 3, unmatched: 3, truncated: false },
      "None of this scope's processes carry a process generation, so their activity cannot be read."],
    [{ applied: true, processes: 3, unmatched: 1, truncated: false },
      "1 process carries no process generation, so their activity is not counted."],
    [{ applied: true, processes: 3, unmatched: 0, truncated: false }, null],
  ])("says how much of %o the activity reflects", (scope, want) => {
    expect(scopeNote(scope)).toBe(want);
  });
});
