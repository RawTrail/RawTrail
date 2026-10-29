import { describe, it, expect } from "vitest";

import { formatSpan, formatTokens, usageLine } from "./sessionUsage";
import type { AgentUsageTotals } from "../../types";

const model = (name: string, input: number, output: number, cacheWrite: number, cacheRead: number) => ({
  model: name, messages: 1, input_tokens: input, output_tokens: output, cache_creation_input_tokens: cacheWrite,
  cache_read_input_tokens: cacheRead, thinking_tokens: 0,
});

describe("usageLine", () => {
  // spec:web-ui/agent-session-header-usage/the-header-says-what-the-session-s-messages-used
  it("sums every model's tokens and names each mode", () => {
    const usage: AgentUsageTotals = {
      models: [model("claude-opus-5-5", 10, 300, 1000, 20_000), model("claude-sonnet-5-5", 5, 100, 0, 500)],
      modes: [
        { mode: "", messages: 1, first_ns: 0, last_ns: 0 },
        { mode: "auto", messages: 3, first_ns: 0, last_ns: 120e9 },
        { mode: "bypassPermissions", messages: 2, first_ns: 0, last_ns: 3_900e9 },
        { mode: "somethingNew", messages: 1, first_ns: 0, last_ns: 0 },
      ],
    };
    expect(usageLine(usage)).toEqual({
      models: ["claude-opus-5-5", "claude-sonnet-5-5"],
      total: 21_915, input: 15, cache: 21_500, output: 400,
      modes: [
        { mode: "", label: "Mode not recorded", messages: 1, spanNs: 0 },
        { mode: "auto", label: "Auto", messages: 3, spanNs: 120e9 },
        { mode: "bypassPermissions", label: "Bypass permissions", messages: 2, spanNs: 3_900e9 },
        { mode: "somethingNew", label: "somethingNew", messages: 1, spanNs: 0 },
      ],
    });
  });

  it("says nothing for a session that recorded no message's usage", () => {
    expect(usageLine(undefined)).toBeNull();
    expect(usageLine({ models: [], modes: [] })).toBeNull();
  });
});

describe("formatTokens and formatSpan", () => {
  it.each([[950, "950"], [1000, "1k"], [12_340, "12.3k"], [999_400, "999k"], [4_100_000, "4.1M"], [250_000_000, "250M"]])(
    "%d tokens read as %s", (n, want) => { expect(formatTokens(n)).toBe(want); });

  it.each([[10e9, "under a minute"], [120e9, "2 min"], [3_600e9, "1 h"], [3_900e9, "1 h 5 min"]])(
    "%d ns read as %s", (ns, want) => { expect(formatSpan(ns)).toBe(want); });
});
