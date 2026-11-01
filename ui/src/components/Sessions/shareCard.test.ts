import { describe, expect, it } from "vitest";
import type { AgentSessionDigest, AgentSessionTotals, Alert } from "../../types";
import { altText, caption, domainCount, filesNotes, shareCard, type ShareInput } from "./shareCard";
import type { Identities } from "./shareRedact";

const MINUTE_NS = 60e9;

function digest(over: Partial<AgentSessionDigest> = {}): AgentSessionDigest {
  return {
    prompts: 0, programs: [], directories: [], directories_truncated: false,
    files: { changed: 0, outside_project: 0, temporary: 0, agent_files: 0 }, domains: [], domains_truncated: false,
    timeline: [0, 0], command_timeline: [0, 0], timeline_start_ns: 0, timeline_bucket_ns: MINUTE_NS, ...over,
  };
}

function totals(over: Partial<AgentSessionTotals> = {}): AgentSessionTotals {
  return {
    commands: 0, processes: 0, alerts: { total: 0, by_severity: {} }, tool_calls: { total: 0, by_tool: [] },
    activity: { files: { events: 0, distinct: 0, by_operation: {} }, network: { events: 0, distinct: 0 }, dns: { events: 0, distinct: 0 } },
    digest: digest(), ...over,
  };
}

function stoppedRead(id: number, credential: string): Alert {
  return {
    id, host_id: "h", rule_id: "agent_credential_read", source: "detection", severity: "high", title: "AI agent read a credential file",
    description: `An AI agent's /usr/bin/cat was stopped from reading ${credential} (/Users/ann/.aws/credentials)`,
  } as Alert;
}

const IDENTITIES: Identities = { usernames: ["ann"], hostnames: ["anns-mbp.example.com"] };

function input(over: Partial<ShareInput> = {}): ShareInput {
  return {
    totals: totals({
      commands: 412,
      processes: 903,
      tool_calls: { total: 1286, by_tool: [] },
      activity: {
        files: { events: 312, distinct: 48, by_operation: {} }, network: { events: 5, distinct: 3 }, dns: { events: 9, distinct: 4 },
      },
      usage: {
        models: [{ model: "claude-opus-5-5", messages: 9, input_tokens: 100_000, output_tokens: 1_000_000, cache_creation_input_tokens: 0,
          cache_read_input_tokens: 2_000_000, thinking_tokens: 0 }],
        modes: [],
      },
      digest: digest({
        prompts: 6,
        programs: [{ program: "/opt/homebrew/bin/go", count: 132 }, { program: "git", count: 61 }, { program: "rg", count: 44 },
          { program: "npm", count: 18 }, { program: "make", count: 2 }],
        files: { changed: 41, outside_project: 4, temporary: 12, agent_files: 60 },
        domains: [{ domain: "api.anthropic.com", lookups: 3, connections: 2, new: false },
          { domain: "github.com", lookups: 2, connections: 1, new: false },
          { domain: "wiki.example.com", lookups: 1, connections: 1, new: true },
          { domain: "10.0.0.7", lookups: 0, connections: 1, new: false },
          { domain: "nas.local", lookups: 1, connections: 0, new: false }],
        command_timeline: [1, 4, 2],
      }),
    }),
    alerts: [stoppedRead(1, "your AWS credentials")],
    agent: "Claude Code",
    project: "/Users/ann/src/webhooks",
    firstNs: 0,
    lastNs: 47 * MINUTE_NS,
    identities: IDENTITIES,
    ...over,
  };
}

// spec:web-ui/a-shared-session-image-carries-no-identifying-detail/the-card-shows-no-title-project-or-internal-domain
describe("shareCard", () => {
  it("shows what the log reports beside what ran, the files changed, program names and public domains, and nothing it may not", () => {
    const card = shareCard(input());
    expect(card).toEqual({
      agent: "Claude Code", date: expect.any(String) as string, duration: "47 min", tokens: "3.1M", toolCalls: 1286, processes: 903,
      filesChanged: 41, outsideProject: 4, temporary: 12, domains: 2, domainsCut: false,
      programs: [{ name: "go", count: 132 }, { name: "git", count: 61 }, { name: "rg", count: 44 }, { name: "npm", count: 18 }],
      // The internal wiki and the .local name are not shown or counted, and neither is the address.
      destinations: [{ label: "Model APIs", count: 1, domains: ["api.anthropic.com"] }, { label: "Git hosts", count: 1, domains: ["github.com"] }],
      guard: { credentials: ["my AWS credentials"] },
    });
    expect(JSON.stringify(card)).not.toContain("webhooks");
  });

  it("says a short session's length in seconds, never as a phrase", () => {
    expect(shareCard(input({ lastNs: 26e9 })).duration).toBe("26s");
    expect(shareCard(input({ lastNs: 0 })).duration).toBe("1s");
    // Seconds hold until a whole minute, though half of one would round up to it.
    for (const [seconds, said] of [[30, "30s"], [45, "45s"], [59.9, "59s"], [60, "1 min"]] as const) {
      expect(shareCard(input({ lastNs: seconds * 1e9 })).duration).toBe(said);
    }
    expect(shareCard(input({ lastNs: 61 * MINUTE_NS })).duration).toBe("1 h 1 min");
  });

  it("redacts a path before taking its name, so a home folder never becomes a username", () => {
    const nobody: Identities = { usernames: [], hostnames: [] };
    const session = (program: string) => input({
      identities: nobody, totals: totals({ digest: digest({ programs: [{ program, count: 2 }, { program: "/usr/bin/git", count: 1 }] }) }),
    });
    for (const home of ["/Users/ann", "/Users/ann/", "C:\\Users\\Ann Smith"]) {
      const card = shareCard(session(home));
      expect(card.programs).toEqual([{ name: "git", count: 1 }]);
      expect(JSON.stringify(card).toLowerCase()).not.toContain("ann");
    }
    expect(shareCard(session("/Users/ann/bin/tool")).programs).toEqual([{ name: "tool", count: 2 }, { name: "git", count: 1 }]);
  });

  it("names a group's first domains that survive redaction, counts all it holds, and leaves off a group with none", () => {
    const domain = (name: string) => ({ domain: name, lookups: 1, connections: 0, new: false });
    const card = shareCard(input({
      totals: totals({ digest: digest({ domains: [domain("anns-mbp.example.org"), domain("anns-mbp.io"), domain("example.net"),
        domain("anns-mbp.github.com")] }) }),
    }));
    expect(card.destinations).toEqual([{ label: "Everything else", count: 3, domains: ["example.net"] }]);
  });

  it("names the credentials the guard stopped, once each, however many alerts", () => {
    const card = shareCard(input({ alerts: [stoppedRead(1, "your AWS credentials"), stoppedRead(2, "your AWS credentials"),
      stoppedRead(3, "your SSH private key")] }));
    expect(card.guard).toEqual({ credentials: ["my AWS credentials", "my SSH private key"] });
  });

  it("reads a session with no digest, usage or project", () => {
    const card = shareCard(input({ totals: totals({ digest: undefined, usage: undefined }), project: undefined, alerts: [],
      firstNs: undefined }));
    expect(card).toMatchObject({ date: null, duration: null, tokens: null, filesChanged: 0, outsideProject: null, temporary: 0, domains: 0,
      programs: [], destinations: [], guard: null });
    expect(filesNotes(card)).toEqual([]);
  });

  it("notes what is outside the project and the temporary files left out, and counts a cut domain list as at least its length", () => {
    const card = shareCard(input());
    expect(filesNotes(card)).toEqual(["4 outside the project", "12 temporary files not counted"]);
    expect(domainCount(card)).toBe("2");
    const cut = shareCard(input({ totals: totals({ digest: digest({ domains: input().totals.digest?.domains, domains_truncated: true }) }) }));
    expect(domainCount(cut)).toBe("2+");
  });
});

describe("caption and altText", () => {
  it("say what the card says and nothing else", () => {
    const card = shareCard(input());
    expect(caption(card)).toBe("My AI agent's log shows 1,286 tool calls. The kernel saw 903 processes run. "
      + "The credential guard stopped it reading my AWS credentials. Recorded with RawTrail.");
    // The date is written in the browser's locale, so it is matched loosely and everything around it exactly.
    const alt = altText(card);
    expect(alt).toMatch(/^A summary card of a Claude Code session on .+, running 47 min, 3\.1M tokens: /);
    expect(alt.slice(alt.indexOf(": ") + 2)).toBe("What my AI agent did. 1,286 tool calls in its log, 903 processes ran. "
      + "41 files changed, 4 outside the project, 12 temporary files not counted. 2 domains contacted. "
      + "Programs it ran most: go (132), git (61), rg (44) and npm (18). "
      + "It talked to model apis (1: api.anthropic.com) and git hosts (1: github.com). "
      + "The credential guard stopped it reading my AWS credentials.");
  });
});

// rng is a small seeded generator (mulberry32), so the property below draws the same inputs on every run and a failure reproduces.
function rng(seed: number): () => number {
  let a = seed;
  return () => {
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

// spec:web-ui/a-shared-session-image-carries-no-identifying-detail/no-username-hostname-or-address-survives
describe("a shared card names no person or machine", () => {
  it("for any session carrying usernames, hostnames and addresses in any field, none reaches the card, caption or alt text", () => {
    const random = rng(1430);
    const pick = <T,>(xs: readonly T[]): T => xs[Math.floor(random() * xs.length)];
    const token = (n: number): string => Array.from({ length: n }, () => pick("abcdefghjkmnpqrstuvwxyz23456789".split(""))).join("");
    const octet = (): number => Math.floor(random() * 256);
    for (let run = 0; run < 500; run++) {
      // Each name is distinctive (its prefix and length make an accidental match with the card's own words impossible), so finding it
      // anywhere in the output is a leak.
      const user = `u${token(6)}`;
      const label = `h${token(6)}`;
      const host = pick([`${label}.${token(5)}.com`, `${label}.local`, `${label}.corp`, label]);
      const ipv4 = `${String(octet())}.${String(octet())}.${String(octet())}.${String(octet())}`;
      const ipv6 = `fd${token(2).replace(/[^0-9a-f]/g, "1")}::${String(octet())}:1`;
      const secrets = [user, label, host, ipv4, ipv6];
      const anywhere = (): string => pick([user, host, label, ipv4, ipv6, `${user}@${host}`, `/Users/${user}`, `/home/${user}`,
        `C:\\Users\\${user} ${token(4)}\\repo`, `backup_${ipv4}`, `backup_${ipv6}`, user.toUpperCase()]);
      const sentence = (): string => `${token(4)} ${anywhere()} ${token(3)} ${anywhere()}`;
      const session = input({
        agent: pick(["Claude Code", `Claude Code on ${host}`, `${user}'s agent`, sentence()]),
        project: pick([`/Users/${user}/src/${token(5)}`, `/home/${user}/${label}`, `/srv/${host}/${user}`]),
        identities: { usernames: [user], hostnames: [host] },
        totals: totals({
          commands: Math.floor(random() * 1000),
          tool_calls: { total: Math.floor(random() * 1000), by_tool: [] },
          digest: digest({
            programs: [{ program: `/Users/${user}/bin/${token(4)}`, count: 3 }, { program: pick([label, `${label}-cli`, user]), count: 2 },
              { program: `ssh ${ipv4}`, count: 1 }],
            domains: [{ domain: host, lookups: 1, connections: 1, new: false }, { domain: ipv4, lookups: 0, connections: 1, new: false },
              { domain: `${label}.github.io`, lookups: 1, connections: 0, new: false },
              { domain: `${user}.${token(4)}.local`, lookups: 1, connections: 0, new: false }],
          }),
        }),
      });
      const card = shareCard(session);
      const out = [JSON.stringify(card), caption(card), altText(card)].join("\n").toLowerCase();
      for (const s of secrets) expect(out, `run ${String(run)} leaked ${s}`).not.toContain(s.toLowerCase());
    }
  });
});
