// Every sentence the host timeline puts in front of an analyst about how far its alert-chain scope reaches. Split out of
// HostTimeline.tsx because a component module that also exports constants breaks fast refresh, and because tests assert on this list
// rather than on the markup rendering it.
//
// The server resolves the chain and reports how far the scope reaches (issue #1282), so each sentence follows from that report alone.
// None claims more than the report says.

import type { TimelineScope } from "../types";

export const scopedMessage = "Scoped to the alert chain";

// The scope could not be applied: the events shown are the whole host's.
export const notStoredMessage =
  "Showing the whole host: this alert's process is no longer stored, so there is no chain to narrow to.";
export const noGenerationsMessage =
  "Showing the whole host: this alert's processes carry no generation data, so the timeline cannot narrow to the chain.";

// The scope applied but reaches only part of the chain, which is worse than showing too much: nothing about a narrowed list
// suggests part of it is absent.
export const partialScopeMessage =
  "Scoped to part of the alert chain: some of its processes carry no generation data, so their events are not listed.";
export const truncatedScopeMessage =
  "Scoped to part of the alert chain: it has more processes than the timeline can scope to, so some events are not listed.";

// degradedScopeMessages is every sentence the timeline renders in the degraded style. Exported so a test asserting the timeline said
// nothing about its scope can enumerate them instead of naming a CSS class, which passes when the class is renamed.
export const degradedScopeMessages: readonly string[] = [
  notStoredMessage,
  noGenerationsMessage,
  partialScopeMessage,
  truncatedScopeMessage,
];

// chainScopeNote turns the server's report into the sentence the timeline shows and whether it is a warning. A truncated scope is
// reported ahead of unmatched processes: it can hide far more events, since every process past the cap is left out whether or not it
// carries a generation.
export function chainScopeNote(scope: TimelineScope): { message: string; degraded: boolean } {
  if (!scope.applied) {
    return { message: scope.processes === 0 ? notStoredMessage : noGenerationsMessage, degraded: true };
  }
  if (scope.truncated) return { message: truncatedScopeMessage, degraded: true };
  if (scope.unmatched > 0) return { message: partialScopeMessage, degraded: true };
  return { message: scopedMessage, degraded: false };
}
