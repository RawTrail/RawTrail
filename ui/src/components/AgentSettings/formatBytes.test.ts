import { describe, it, expect } from "vitest";
import { formatBytes } from "./formatBytes";

describe("formatBytes", () => {
  it("writes a size in the largest decimal unit it reaches, to one place", () => {
    expect(formatBytes(0)).toBe("0 bytes");
    expect(formatBytes(1)).toBe("1 byte");
    expect(formatBytes(999)).toBe("999 bytes");
    expect(formatBytes(1000)).toBe("1.0 KB");
    expect(formatBytes(12_345_678)).toBe("12.3 MB");
    expect(formatBytes(110_000_000_000)).toBe("110.0 GB");
    expect(formatBytes(2_500_000_000_000)).toBe("2.5 TB");
  });
});
