// What an AI agent session's messages used (issue #1427), as the session header says it: the models, the tokens, and the permission
// modes the agent ran in.

import type { AgentUsageTotals } from "../../types";

// MODE_LABELS names the permission modes Claude Code records. A mode it adds later reads as recorded.
const MODE_LABELS: ReadonlyMap<string, string> = new Map([
  ["acceptEdits", "Accept edits"],
  ["auto", "Auto"],
  ["bypassPermissions", "Bypass permissions"],
  ["default", "Default"],
  ["dontAsk", "Don't ask"],
  ["manual", "Manual"],
  ["plan", "Plan"],
]);

const THOUSAND = 1000;
const MILLION = 1_000_000;
const MINUTE_NS = 60e9;
const MINUTES_PER_HOUR = 60;

// BYPASS_MODE is the mode in which the agent runs tools without asking, the one an operator looks for first.
export const BYPASS_MODE = "bypassPermissions";

export interface ModeLine {
  mode: string;
  label: string;
  messages: number;
  spanNs: number;
}

export interface UsageLine {
  models: string[];
  // total is every token the session's messages read or wrote; input and cache are its input tokens apart from and through the
  // prompt cache, and output its output tokens, thinking included.
  total: number;
  input: number;
  cache: number;
  output: number;
  modes: ModeLine[];
}

// usageLine sums a session's usage for its header, or null when the session recorded no message's usage.
export function usageLine(usage: AgentUsageTotals | undefined): UsageLine | null {
  if (!usage || usage.models.length === 0) return null;
  let input = 0;
  let cache = 0;
  let output = 0;
  for (const m of usage.models) {
    input += m.input_tokens;
    cache += m.cache_creation_input_tokens + m.cache_read_input_tokens;
    output += m.output_tokens;
  }
  return {
    models: usage.models.map((m) => m.model),
    total: input + cache + output,
    input,
    cache,
    output,
    modes: usage.modes.map((m) => ({
      mode: m.mode,
      label: m.mode === "" ? "Mode not recorded" : (MODE_LABELS.get(m.mode) ?? m.mode),
      messages: m.messages,
      spanNs: m.last_ns - m.first_ns,
    })),
  };
}

// formatTokens writes a token count the way a person reads one: 950, 12.3k, 4.1M.
export function formatTokens(n: number): string {
  if (n < THOUSAND) return String(n);
  if (n < MILLION) return `${trim(n / THOUSAND)}k`;
  return `${trim(n / MILLION)}M`;
}

function trim(n: number): string {
  return n >= 100 ? String(Math.round(n)) : n.toFixed(1).replace(/\.0$/, "");
}

// formatSpan writes how long a mode was seen for: under a minute, minutes, or hours and minutes.
export function formatSpan(spanNs: number): string {
  const minutes = Math.round(spanNs / MINUTE_NS);
  if (minutes < 1) return "under a minute";
  if (minutes < MINUTES_PER_HOUR) return `${String(minutes)} min`;
  const hours = Math.floor(minutes / MINUTES_PER_HOUR);
  const rest = minutes % MINUTES_PER_HOUR;
  return rest === 0 ? `${String(hours)} h` : `${String(hours)} h ${String(rest)} min`;
}
