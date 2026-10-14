// Turning the process search's URL filter into the endpoint's (issue #1289). Every chip is passed through as it is, but for "since",
// which names a reach back from now and becomes the window's start.

import type { ProcessSearchFilter } from "../../api";
import { NANOSECONDS_PER_MILLISECOND } from "../../constants";

const HOUR_MS = 3_600_000;
const HOURS_PER_DAY = 24;
const DAY_MS = HOURS_PER_DAY * HOUR_MS;
const DAYS_PER_WEEK = 7;
const DAYS_PER_MONTH = 30;

// SINCE_CHOICES are the "Since" chip's options and how far back each reaches.
export const SINCE_CHOICES: readonly { value: string; label: string; reachMs: number }[] = [
  { value: "1h", label: "Last hour", reachMs: HOUR_MS },
  { value: "24h", label: "Last day", reachMs: DAY_MS },
  { value: "7d", label: "Last week", reachMs: DAYS_PER_WEEK * DAY_MS },
  { value: "30d", label: "Last 30 days", reachMs: DAYS_PER_MONTH * DAY_MS },
];

// searchFilter is the endpoint's filter for the URL's: a "since" choice becomes from, nowMs minus its reach, in ns. A "since" that names
// no choice (a hand-edited URL) is dropped rather than read as some window.
export function searchFilter(active: Record<string, string>, nowMs: number): ProcessSearchFilter {
  const { since, ...rest } = active;
  const choice = SINCE_CHOICES.find((c) => c.value === since);
  if (!choice) return rest;
  return { ...rest, from: String((nowMs - choice.reachMs) * NANOSECONDS_PER_MILLISECOND) };
}
