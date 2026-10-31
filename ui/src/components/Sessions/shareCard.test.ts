import { describe, expect, it } from "vitest";
import type { AgentSessionDigest, AgentSessionTotals, Alert } from "../../types";
import { altText, caption, DEFAULT_SHARE_OPTIONS, headline, shareCard, type ShareInput, type ShareOptions } from "./shareCard";
import { HIDDEN, type Identities } from "./shareRedact";

const MINUTE_NS = 60e9;

function digest(over: Partial<AgentSessionDigest> = {}): AgentSessionDigest {
  return {
    prompts: 0, programs: [], directories: [], directories_truncated: false, outside_project: 0, domains: [], domains_truncated: false,
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
        outside_project: 0,
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

// spec:web-ui/a-shared-session-image-carries-no-identifying-detail/the-card-shows-only-what-the-person-chose
describe("shareCard", () => {
  it("shows counts, program names and public domains, and hides the project, internal domains and addresses by default", () => {
    const card = shareCard(input(), DEFAULT_SHARE_OPTIONS);
    expect(card).toMatchObject({
      agent: "Claude Code", title: null, project: null, duration: "47 min", prompts: 6, commands: 412, toolCalls: 1286, tokens: "3.1M",
      timeline: [1, 4, 2], fileEvents: 312, paths: 48, outsideProject: 0,
    });
    expect(card.programs).toEqual([{ name: "go", count: 132 }, { name: "git", count: 61 }, { name: "rg", count: 44 },
      { name: "npm", count: 18 }]);
    expect(card.destinations).toEqual([
      { label: "Model APIs", count: 1, domains: ["api.anthropic.com"] },
      { label: "Git hosts", count: 1, domains: ["github.com"] },
    ]);
    expect(card.guard).toEqual({ credentials: ["my AWS credentials"] });
  });

  it("shows what the person chose: a redacted title, the project's name, internal domains", () => {
    const options: ShareOptions = { title: "Fix the deploy from anns-mbp to 10.0.0.7", showProject: true, showInternalDomains: true,
      showGuard: false };
    const card = shareCard(input(), options);
    expect(card.title).toBe(`Fix the deploy from ${HIDDEN} to ${HIDDEN}`);
    expect(card.project).toBe("webhooks");
    expect(card.destinations.map((d) => d.label)).toContain("Everything else");
    // An internal domain the person chose to show still names no host: the host's own name is removed, so it is left off.
    expect(card.destinations.flatMap((d) => d.domains)).not.toContain("10.0.0.7");
    expect(card.guard).toBeNull();
  });

  it("drops a title the person emptied, and cuts a long one", () => {
    expect(shareCard(input(), { ...DEFAULT_SHARE_OPTIONS, title: "   " }).title).toBeNull();
    expect(shareCard(input(), { ...DEFAULT_SHARE_OPTIONS, title: "x".repeat(200) }).title).toHaveLength(90);
  });

  it("redacts a title before cutting it, so the cut cannot leave part of a name", () => {
    const title = `${"x".repeat(85)} anns-mbp.example.com`;
    const card = shareCard(input(), { ...DEFAULT_SHARE_OPTIONS, title });
    expect(card.title).toBe(`${"x".repeat(85)} ${HIDDEN}`);
    expect(card.title).not.toContain("ann");
  });

  it("names the credentials the guard stopped, once each, however many alerts", () => {
    const card = shareCard(input({ alerts: [stoppedRead(1, "your AWS credentials"), stoppedRead(2, "your AWS credentials"),
      stoppedRead(3, "your SSH private key")] }), DEFAULT_SHARE_OPTIONS);
    expect(card.guard).toEqual({ credentials: ["my AWS credentials", "my SSH private key"] });
  });

  it("reads a session with no digest, usage or project", () => {
    const card = shareCard(input({ totals: totals({ digest: undefined, usage: undefined }), project: undefined, alerts: [],
      firstNs: undefined }), DEFAULT_SHARE_OPTIONS);
    expect(card).toMatchObject({ duration: null, tokens: null, timeline: [], timelineStart: null, outsideProject: null, guard: null,
      date: null });
    expect(headline(card)).toBe("What my AI agent did");
  });
});

describe("caption and altText", () => {
  it("say what the card says and nothing else", () => {
    const card = shareCard(input(), DEFAULT_SHARE_OPTIONS);
    expect(caption(card)).toBe("47 min, 412 commands and 1,286 tool calls: what my AI agent actually did, recorded at the kernel. "
      + "The credential guard stopped it reading my AWS credentials. Recorded with RawTrail.");
    // The date is written in the browser's locale, so it is matched loosely and everything around it exactly.
    const alt = altText(card);
    expect(alt).toMatch(/^A summary card of a Claude Code session on [^:]+: /);
    expect(alt.slice(alt.indexOf(": ") + 2)).toBe("What my AI agent did in 47 min. Ran for 47 min, 412 commands, 1,286 tool calls, 3.1M tokens. "
      + "Programs it ran most: go, git, rg and npm. It talked to model apis (1) and git hosts (1). "
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
        agent: pick(["Claude Code", `Claude Code on ${host}`, `${user}'s agent`]),
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
      const options: ShareOptions = { title: sentence(), showProject: random() < 0.5, showInternalDomains: random() < 0.5,
        showGuard: random() < 0.5 };
      const card = shareCard(session, options);
      const out = [JSON.stringify(card), caption(card), altText(card)].join("\n").toLowerCase();
      for (const s of secrets) expect(out, `run ${String(run)} leaked ${s}`).not.toContain(s.toLowerCase());
    }
  });
});
