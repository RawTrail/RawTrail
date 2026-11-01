// Where an AI agent session talked to, by organization, as its Summary card groups the summary endpoint's domains (issue #1429).

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
