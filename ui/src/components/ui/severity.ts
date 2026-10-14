import type { BadgeVariant } from "./Badge";

// Map rather than an object literal: severity is a server-supplied string, and an object lookup would resolve inherited keys such as
// "constructor" to something that is not a variant at all.
const SEVERITY_VARIANTS: ReadonlyMap<string, BadgeVariant> = new Map<string, BadgeVariant>([
  ["critical", "critical"],
  ["high", "high"],
  ["medium", "medium"],
  ["low", "low"],
]);

// SEVERITY_ORDER lists the severities most- to least-severe, the one ordering every surface ranks severities by.
export const SEVERITY_ORDER = ["critical", "high", "medium", "low"] as const;

// severityRank returns a severity's position in SEVERITY_ORDER (0 = critical). A severity that is not one of the four (an unset "" or an
// unrecognized value) ranks last.
export function severityRank(sev: string): number {
  const i = SEVERITY_ORDER.indexOf(sev as (typeof SEVERITY_ORDER)[number]);
  return i === -1 ? SEVERITY_ORDER.length : i;
}

// severityBadgeVariant is the one mapping from an alert or rule severity to its badge, shared by every surface that shows one, so a new
// severity or a change of treatment cannot land on some of them and not others. An unrecognised severity renders neutral.
export function severityBadgeVariant(severity: string): BadgeVariant {
  return SEVERITY_VARIANTS.get(severity) ?? "neutral";
}
