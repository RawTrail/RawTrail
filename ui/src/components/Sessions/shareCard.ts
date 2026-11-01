// What a session shared as an image says (issue #1430): a card built from the Summary's counts, the programs its commands ran, and
// the public domains it talked to, every string redacted. The card is the only input the image renderer gets, so nothing that is
// not on it can reach a shared image.

import type { AgentSessionTotals, Alert, DomainActivity } from "../../types";
import { basename, plural } from "../Search/format";
import { credentialReads } from "./credentialRead";
import { groupDomains } from "./sessionDigest";
import { formatSpan, formatTokens, usageLine } from "./sessionUsage";
import { HIDDEN, isAddress, isInternalDomain, redact, type Identities } from "./shareRedact";

const NANOSECONDS_PER_MILLISECOND = 1e6;
// A card lists this many programs and destination groups, and this many domains in each group: what fits a phone-sized image.
const CARD_PROGRAMS = 4;
const CARD_GROUPS = 4;
const CARD_DOMAINS = 2;
// A title is one prompt, edited down to what fits three lines of the card.
export const TITLE_MAX = 90;

// ShareOptions are what the person chose to show. Everything else on the card is always shown, and everything not on it never is.
export interface ShareOptions {
  // title is the prompt the person chose to show as the card's title, as they edited it, or null to show none.
  readonly title: string | null;
  readonly showProject: boolean;
  readonly showInternalDomains: boolean;
  readonly showGuard: boolean;
}

export const DEFAULT_SHARE_OPTIONS: ShareOptions = { title: null, showProject: false, showInternalDomains: false, showGuard: true };

export interface CardDestination {
  readonly label: string;
  readonly count: number;
  readonly domains: readonly string[];
}

// ShareCard is everything a shared image shows.
export interface ShareCard {
  readonly agent: string;
  readonly date: string | null;
  readonly title: string | null;
  readonly project: string | null;
  readonly duration: string | null;
  readonly prompts: number;
  readonly commands: number;
  readonly toolCalls: number;
  readonly tokens: string | null;
  readonly timeline: readonly number[];
  readonly timelineStart: string | null;
  readonly timelineEnd: string | null;
  readonly programs: readonly { name: string; count: number }[];
  readonly destinations: readonly CardDestination[];
  readonly fileEvents: number;
  readonly paths: number;
  // outsideProject is how many paths the session touched outside its project, or null when it has no project.
  readonly outsideProject: number | null;
  // guard is the credentials the credential guard stopped the agent reading, or null when it stopped none or the person chose not to
  // show it. It says which credentials, not how many reads: the rule folds a session's later reads of one credential into its first
  // alert.
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

function time(ns: number): string {
  return new Date(ns / NANOSECONDS_PER_MILLISECOND).toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
}

// shareCard builds the card for a session. A program is named by its last path segment, never with its arguments; a domain that is an
// address is never shown, nor an internal one unless the person chose to; and every string is redacted.
export function shareCard(input: ShareInput, options: ShareOptions): ShareCard {
  const { totals, identities } = input;
  const digest = totals.digest;
  const clean = (s: string): string => redact(s, identities);
  // Redacted whole, then cut: cutting first can leave part of a name that redaction no longer recognizes.
  // Cut by code point, so an emoji is never split into a half that cannot be drawn or encoded.
  const title = options.title === null ? null : Array.from(clean(options.title)).slice(0, TITLE_MAX).join("").trim();
  const files = totals.activity?.files;
  const usage = usageLine(totals.usage);
  const timeline = digest?.command_timeline ?? [];
  const endNs = digest ? digest.timeline_start_ns + digest.timeline_bucket_ns * timeline.length : undefined;
  return {
    agent: clean(input.agent),
    date: input.firstNs === undefined ? null : date(input.firstNs),
    title: title === "" ? null : title,
    project: options.showProject && input.project ? named(basename(trimSlashes(clean(input.project)))) : null,
    duration: input.firstNs !== undefined && input.lastNs !== undefined ? formatSpan(input.lastNs - input.firstNs) : null,
    prompts: digest?.prompts ?? 0,
    commands: totals.commands,
    toolCalls: totals.tool_calls.total,
    tokens: usage ? formatTokens(usage.total) : null,
    timeline,
    timelineStart: digest && timeline.length > 0 ? time(digest.timeline_start_ns) : null,
    timelineEnd: endNs !== undefined && timeline.length > 0 ? time(endNs) : null,
    // A path is redacted whole before its last segment is taken: /Users/ann has to read ~ before it can be cut to a name.
    programs: (digest?.programs ?? []).slice(0, CARD_PROGRAMS).map((p) => ({ name: named(basename(clean(p.program))), count: p.count }))
      .filter((p): p is { name: string; count: number } => p.name !== null),
    destinations: destinations(digest?.domains ?? [], options, identities),
    fileEvents: files?.events ?? 0,
    paths: files?.distinct ?? 0,
    outsideProject: input.project ? (digest?.files.outside_project ?? 0) : null,
    guard: options.showGuard ? guard(input.alerts) : null,
  };
}

// named is a name that says something, or null for one redaction emptied: nothing, a removal, or a bare home folder.
function named(name: string): string | null {
  return name === "" || name === HIDDEN || name === "~" ? null : name;
}

function trimSlashes(path: string): string {
  let end = path.length;
  while (end > 1 && path[end - 1] === "/") end--;
  return path.slice(0, end);
}

// destinations groups the domains a card may show: never an address, an internal one only when the person chose to, and by name only a
// domain redaction left whole, since a partly removed name says nothing and may still hint at one. Each group names the first of its
// domains that survive, counts all it holds, and is left off when none survives.
function destinations(domains: readonly DomainActivity[], options: ShareOptions, identities: Identities): CardDestination[] {
  const shown = domains.filter((d) => !isAddress(d.domain) && (options.showInternalDomains || !isInternalDomain(d.domain, identities)));
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

// headline is the card's title when the person chose none: what the agent did and for how long.
export function headline(card: ShareCard): string {
  return card.duration ? `What my AI agent did in ${card.duration}` : "What my AI agent did";
}

function list(items: readonly string[]): string {
  if (items.length <= 1) return items.join("");
  return `${items.slice(0, -1).join(", ")} and ${items[items.length - 1]}`;
}

// caption is a suggested post to go with the image, built from the card alone.
export function caption(card: ShareCard): string {
  const counts = list([
    ...(card.duration ? [card.duration] : []),
    plural(card.commands, "command", "commands"),
    plural(card.toolCalls, "tool call", "tool calls"),
  ]);
  const guard = card.guard ? ` The credential guard stopped it reading ${list([...card.guard.credentials])}.` : "";
  return `${counts}: what my AI agent actually did, recorded at the kernel.${guard} Recorded with RawTrail.`;
}

// altText reads the card out for a screen reader, from the card alone.
export function altText(card: ShareCard): string {
  const where = (card.project ? " in " + card.project : "") + (card.date ? " on " + card.date : "");
  const counts = [
    card.duration ? "Ran for " + card.duration : "",
    plural(card.commands, "command", "commands"),
    plural(card.toolCalls, "tool call", "tool calls"),
    card.tokens ? card.tokens + " tokens" : "",
  ].filter((c) => c !== "");
  const parts = [`${card.title ?? headline(card)}.`, `${counts.join(", ")}.`];
  if (card.programs.length > 0) parts.push(`Programs it ran most: ${list(card.programs.map((p) => p.name))}.`);
  if (card.destinations.length > 0) {
    const groups = card.destinations.map((d) => `${d.label.toLowerCase()} (${String(d.count)})`);
    parts.push(`It talked to ${list(groups)}.`);
  }
  if (card.guard) parts.push(`The credential guard stopped it reading ${list([...card.guard.credentials])}.`);
  return `A summary card of a ${card.agent} session${where}: ${parts.join(" ")}`;
}
