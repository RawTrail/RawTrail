// What a session's Summary shows, and the image it is shared as says (issues #1429, #1430): a card built from the session summary's
// counts, the programs its commands ran and the public domains it talked to, every string redacted. The card is the only input the
// image renderer gets, so nothing that is not on it can reach a shared image.

import type { AgentSessionTotals, Alert, DomainActivity } from "../../types";
import { basename, plural } from "../Search/format";
import { credentialReads } from "./credentialRead";
import { groupDomains } from "./sessionDigest";
import { formatTokens, shortSpan, usageLine } from "./sessionUsage";
import { HIDDEN, isAddress, isInternalDomain, redact, type Identities } from "./shareRedact";

const NANOSECONDS_PER_MILLISECOND = 1e6;
// A card lists this many programs and destination groups, and this many domains in each group: what fits a phone-sized image.
const CARD_PROGRAMS = 4;
const CARD_GROUPS = 4;
const CARD_DOMAINS = 2;

// HEADLINE is the card's title. A prompt is never one: what a person typed can name anything.
export const HEADLINE = "What my AI agent did";

export interface CardDestination {
  readonly label: string;
  readonly count: number;
  readonly domains: readonly string[];
}

// ShareCard is everything a session's Summary and its shared image show.
export interface ShareCard {
  readonly agent: string;
  readonly date: string | null;
  // duration is how long the session ran, short enough for the card's heading.
  readonly duration: string | null;
  readonly tokens: string | null;
  // toolCalls are what the agent's own log reports it did; processes are what the sensor saw run.
  readonly toolCalls: number;
  readonly processes: number;
  // filesChanged leaves out Claude Code's own files and the temporary files the session created and removed, which temporary counts.
  readonly filesChanged: number;
  // outsideProject is how many of the files changed are outside the session's project, or null when it has no project.
  readonly outsideProject: number | null;
  readonly temporary: number;
  // domains counts the public domains the card may name; domainsCut says the digest listed only the busiest.
  readonly domains: number;
  readonly domainsCut: boolean;
  readonly programs: readonly { name: string; count: number }[];
  readonly destinations: readonly CardDestination[];
  // guard is the credentials the credential guard stopped the agent reading, or null when it stopped none. It says which credentials,
  // not how many reads: the rule folds a session's later reads of one credential into its first alert.
  readonly guard: { readonly credentials: readonly string[] } | null;
}

export interface ShareInput {
  readonly totals: AgentSessionTotals;
  readonly alerts: readonly Alert[];
  readonly agent: string;
  readonly project: string | undefined;
  readonly firstNs: number | undefined;
  readonly lastNs: number | undefined;
  readonly identities: Identities;
}

function date(ns: number): string {
  return new Date(ns / NANOSECONDS_PER_MILLISECOND).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
}

// shareCard builds the card for a session. A program is named by its last path segment, never with its arguments; a domain that is an
// address or internal is never shown or counted; and every string is redacted.
export function shareCard(input: ShareInput): ShareCard {
  const { totals, identities } = input;
  const digest = totals.digest;
  const clean = (s: string): string => redact(s, identities);
  const usage = usageLine(totals.usage);
  const shown = (digest?.domains ?? []).filter((d) => !isAddress(d.domain) && !isInternalDomain(d.domain, identities));
  return {
    agent: clean(input.agent),
    date: input.firstNs === undefined ? null : date(input.firstNs),
    duration: input.firstNs !== undefined && input.lastNs !== undefined ? shortSpan(input.lastNs - input.firstNs) : null,
    tokens: usage ? formatTokens(usage.total) : null,
    toolCalls: totals.tool_calls.total,
    processes: totals.processes,
    filesChanged: digest?.files.changed ?? 0,
    outsideProject: input.project ? (digest?.files.outside_project ?? 0) : null,
    temporary: digest?.files.temporary ?? 0,
    domains: shown.length,
    domainsCut: digest?.domains_truncated ?? false,
    // A path is redacted whole before its last segment is taken: /Users/ann has to read ~ before it can be cut to a name.
    programs: (digest?.programs ?? []).slice(0, CARD_PROGRAMS).map((p) => ({ name: named(basename(clean(p.program))), count: p.count }))
      .filter((p): p is { name: string; count: number } => p.name !== null),
    destinations: destinations(shown, identities),
    guard: guard(input.alerts),
  };
}

// named is a name that says something, or null for one redaction emptied: nothing, a removal, or a bare home folder.
function named(name: string): string | null {
  return name === "" || name === HIDDEN || name === "~" ? null : name;
}

// destinations groups the public domains, naming by name only a domain redaction left whole, since a partly removed name says nothing
// and may still hint at one. Each group names the first of its domains that survive, counts all it holds, and is left off when none
// survives.
function destinations(shown: readonly DomainActivity[], identities: Identities): CardDestination[] {
  return groupDomains(shown)
    .map((g) => ({
      label: g.label,
      count: g.domains.length,
      domains: g.domains.map((d) => redact(d.domain, identities)).filter((d) => d !== "" && !d.includes(HIDDEN)).slice(0, CARD_DOMAINS),
    }))
    .filter((g) => g.domains.length > 0)
    .slice(0, CARD_GROUPS);
}

function guard(alerts: readonly Alert[]): ShareCard["guard"] {
  const stopped = credentialReads(alerts).filter((r) => r.stopped);
  if (stopped.length === 0) return null;
  return { credentials: [...new Set(stopped.map((r) => mine(r.text.replace(/^Stopped from reading /, ""))))] };
}

// mine words a credential as the person sharing the card says it: the guard's "your AWS credentials" is their "my AWS credentials".
function mine(credential: string): string {
  return credential.replace(/\byour\b/g, "my");
}

function list(items: readonly string[]): string {
  if (items.length <= 1) return items.join("");
  return `${items.slice(0, -1).join(", ")} and ${items[items.length - 1]}`;
}

// domainCount says how many domains a card counts: a cut list counts only the busiest, so it reads as at least that many.
export function domainCount(card: ShareCard): string {
  return `${card.domains.toLocaleString()}${card.domainsCut ? "+" : ""}`;
}

// filesNotes are the lines the card says under its files changed: how many are outside the project and how many temporary files were
// left out, those that apply.
export function filesNotes(card: ShareCard): string[] {
  return [
    card.outsideProject === null ? "" : `${card.outsideProject.toLocaleString()} outside the project`,
    card.temporary > 0 ? `${plural(card.temporary, "temporary file", "temporary files")} not counted` : "",
  ].filter((s) => s !== "");
}

// caption is a suggested post to go with the image, built from the card alone.
export function caption(card: ShareCard): string {
  const guard = card.guard ? ` The credential guard stopped it reading ${list([...card.guard.credentials])}.` : "";
  return `My AI agent's log shows ${plural(card.toolCalls, "tool call", "tool calls")}. The kernel saw `
    + `${plural(card.processes, "process", "processes")} run.${guard} Recorded with RawTrail.`;
}

// altText reads the card out for a screen reader, from the card alone.
export function altText(card: ShareCard): string {
  const when = [
    card.date ? `on ${card.date}` : "",
    card.duration ? `running ${card.duration}` : "",
    card.tokens ? `${card.tokens} tokens` : "",
  ].filter((s) => s !== "").join(", ");
  const parts = [
    `${HEADLINE}.`,
    `${plural(card.toolCalls, "tool call", "tool calls")} in its log, ${plural(card.processes, "process", "processes")} ran.`,
    `${[plural(card.filesChanged, "file changed", "files changed"), ...filesNotes(card)].join(", ")}.`,
    `${domainCount(card)} ${card.domains === 1 && !card.domainsCut ? "domain" : "domains"} contacted.`,
  ];
  if (card.programs.length > 0) {
    parts.push(`Programs it ran most: ${list(card.programs.map((p) => `${p.name} (${p.count.toLocaleString()})`))}.`);
  }
  if (card.destinations.length > 0) {
    const places = card.destinations.map((d) => `${d.label.toLowerCase()} (${d.count.toLocaleString()}: ${d.domains.join(", ")})`);
    parts.push(`It talked to ${list(places)}.`);
  }
  if (card.guard) parts.push(`The credential guard stopped it reading ${list([...card.guard.credentials])}.`);
  return `A summary card of a ${card.agent} session${when ? ` ${when}` : ""}: ${parts.join(" ")}`;
}
