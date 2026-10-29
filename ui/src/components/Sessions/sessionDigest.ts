// What an AI agent session's Summary says about it (issue #1429), shaped from the summary endpoint's digest: where it talked to by
// organization, what it touched by directory, and its timeline.

import type { DomainActivity } from "../../types";

// ORGANIZATIONS names the kinds of destination a Summary groups domains by, in the order it lists them, each with the registrable
// domains that belong to it. A domain none names is under "Everything else".
const ORGANIZATIONS: readonly { label: string; domains: readonly string[] }[] = [
  { label: "Model APIs", domains: ["anthropic.com", "claude.ai", "claude.com", "openai.com"] },
  { label: "Git hosts", domains: ["github.com", "githubusercontent.com", "gitlab.com", "bitbucket.org"] },
  {
    label: "Package registries",
    domains: ["npmjs.org", "npmjs.com", "yarnpkg.com", "pypi.org", "pythonhosted.org", "golang.org", "crates.io", "rubygems.org",
      "maven.org", "brew.sh"],
  },
];
const EVERYTHING_ELSE = "Everything else";

export interface DomainGroup {
  label: string;
  domains: DomainActivity[];
}

// groupDomains groups the digest's domains by organization, in the organizations' order with everything else last, each group's
// domains in the digest's order (busiest first). A domain under a known one belongs with it: the server keeps a name whose registrable
// part is a public suffix whole, as it does objects.githubusercontent.com. An organization none of the domains belongs to is left out.
export function groupDomains(domains: readonly DomainActivity[]): DomainGroup[] {
  const groups = [...ORGANIZATIONS.map((o) => ({ label: o.label, domains: [] as DomainActivity[] })),
    { label: EVERYTHING_ELSE, domains: [] as DomainActivity[] }];
  for (const d of domains) {
    const at = ORGANIZATIONS.findIndex((o) => o.domains.some((known) => d.domain === known || d.domain.endsWith(`.${known}`)));
    groups[at === -1 ? ORGANIZATIONS.length : at].domains.push(d);
  }
  return groups.filter((g) => g.domains.length > 0);
}

// directoryLabel names a directory as the Summary lists it: relative to the session's project when it is the project or under it,
// as recorded otherwise.
export function directoryLabel(directory: string, project: string | undefined): string {
  let root = project ?? "";
  while (root.endsWith("/")) root = root.slice(0, -1);
  if (!root) return directory;
  if (directory === root) return ".";
  return directory.startsWith(`${root}/`) ? `./${directory.slice(root.length + 1)}` : directory;
}

// FILE_OPERATIONS reads the file events' operations as a Summary counts them: an agent's own operation and the watched-path event
// that means the same thing count together.
const FILE_OPERATIONS: readonly { label: string; keys: readonly string[] }[] = [
  { label: "created", keys: ["create"] },
  { label: "modified", keys: ["modify", "file_truncate"] },
  { label: "renamed", keys: ["rename", "file_rename"] },
  { label: "deleted", keys: ["delete", "file_delete"] },
  { label: "opened", keys: ["open"] },
  { label: "connected", keys: ["connect"] },
];

export interface OperationCount {
  label: string;
  count: number;
}

// fileOperations counts file events by what they did, in a fixed order, leaving out what none did.
export function fileOperations(byOperation: Readonly<Record<string, number>>): OperationCount[] {
  const counts = new Map(Object.entries(byOperation));
  return FILE_OPERATIONS
    .map(({ label, keys }) => ({ label, count: keys.reduce((sum, k) => sum + (counts.get(k) ?? 0), 0) }))
    .filter((o) => o.count > 0);
}

// MIN_VISIBLE_BAR_PERCENT keeps a bucket with anything in it visible beside a spike, as the host page's activity strip does.
const MIN_VISIBLE_BAR_PERCENT = 6;
const FULL_PERCENT = 100;

// barHeights is each bucket's bar height in percent of the busiest bucket's: zero for an empty bucket, at least
// MIN_VISIBLE_BAR_PERCENT for any other. A timeline with nothing in it has no bars.
export function barHeights(counts: readonly number[]): number[] {
  const max = Math.max(0, ...counts);
  if (max === 0) return [];
  return counts.map((n) => (n === 0 ? 0 : Math.max(MIN_VISIBLE_BAR_PERCENT, (n / max) * FULL_PERCENT)));
}
