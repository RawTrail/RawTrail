import { describe, expect, it } from "vitest";
import { barHeight } from "./barHeight";

describe("barHeight", () => {
  it.each([
    [0, 400, 0],
    [400, 400, 100],
    [200, 400, 50],
    [1, 400, 6],
  ])("draws %i of a busiest %i at %i%%", (count, max, want) => {
    expect(barHeight(count, max)).toBe(want);
  });
});
