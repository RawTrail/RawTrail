// Turning the process search's URL filter into the endpoint's (issue #1289). Every chip is passed through as it is, but for "since",
// which names a reach back from now and becomes the window's start.

import type { ProcessSearchFilter } from "../../api";
import { MILLISECONDS_PER_DAY, MILLISECONDS_PER_HOUR } from "../../constants";
import { windowBounds } from "../../timewindow";

const DAYS_PER_WEEK = 7;
const DAYS_PER_MONTH = 30;

// SINCE_CHOICES are the "Since" chip's options and how far back each reaches.
export const SINCE_CHOICES: readonly { value: string; label: string; reachMs: number }[] = [
  { value: "1h", label: "Last hour", reachMs: MILLISECONDS_PER_HOUR },
  { value: "24h", label: "Last day", reachMs: MILLISECONDS_PER_DAY },
  { value: "7d", label: "Last week", reachMs: DAYS_PER_WEEK * MILLISECONDS_PER_DAY },
  { value: "30d", label: "Last 30 days", reachMs: DAYS_PER_MONTH * MILLISECONDS_PER_DAY },
];

// searchFilter is the endpoint's filter for the URL's: a "since" choice becomes from and to, the bounds of the relative window that ends
// at nowMs, so every page and its total read one fixed window. A "since" that names no choice (a hand-edited URL) is dropped rather
// than read as some window.
export function searchFilter(active: Record<string, string>, nowMs: number): ProcessSearchFilter {
  const { since, ...rest } = active;
  const choice = SINCE_CHOICES.find((c) => c.value === since);
  if (!choice) return rest;
  const { fromNs, toNs } = windowBounds({ kind: "relative", ms: choice.reachMs }, nowMs);
  return { ...rest, from: String(fromNs), to: String(toNs) };
}
