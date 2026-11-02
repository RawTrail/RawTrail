import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { AgentSessionDigest, AgentSessionTotals, Alert, HostDetail } from "../../types";
import * as api from "../../api";
import { SessionSummary } from "./SessionSummary";
import { sharePng } from "./shareImage";
import { labelled } from "../../test/svgText";

vi.mock("./shareImage", async (original) => ({
  ...await original<typeof import("./shareImage")>(),
  sharePng: vi.fn(() => Promise.resolve(new Blob(["png"], { type: "image/png" }))),
}));

const MINUTE_NS = 60e9;

function digest(over: Partial<AgentSessionDigest> = {}): AgentSessionDigest {
  return {
    prompts: 0, programs: [], directories: [], directories_truncated: false,
    files: { changed: 0, outside_project: 0, temporary: 0, agent_files: 0 }, domains: [], domains_truncated: false,
    timeline: [0, 0], command_timeline: [0, 0], timeline_start_ns: 0, timeline_bucket_ns: 1, ...over,
  };
}

function totals(over: Partial<AgentSessionTotals> = {}): AgentSessionTotals {
  return {
    commands: 0, processes: 0, alerts: { total: 0, by_severity: {} }, tool_calls: { total: 0, by_tool: [] },
    activity: { files: { events: 0, distinct: 0, by_operation: {} }, network: { events: 0, distinct: 0 }, dns: { events: 0, distinct: 0 } },
    digest: digest(), ...over,
  };
}

function busy(): AgentSessionTotals {
  return totals({
    commands: 31,
    processes: 140,
    alerts: { total: 2, by_severity: { high: 2 } },
    tool_calls: { total: 12, by_tool: [{ tool: "Bash", count: 8 }, { tool: "Edit", count: 4 }] },
    activity: {
      files: { events: 9, distinct: 5, by_operation: { create: 2, modify: 6, delete: 1 } },
      network: { events: 3, distinct: 2 },
      dns: { events: 4, distinct: 3 },
    },
    usage: {
      models: [{ model: "claude-opus-5-5", messages: 4, input_tokens: 40, output_tokens: 2_000, cache_creation_input_tokens: 0,
        cache_read_input_tokens: 48_000, thinking_tokens: 0 }],
      modes: [{ mode: "bypassPermissions", messages: 4, first_ns: 0, last_ns: 10 * MINUTE_NS }],
    },
    digest: digest({
      prompts: 5,
      programs: [{ program: "git", count: 7 }, { program: "go", count: 3 }],
      directories: [{ directory: "/Users/dev/app/src", paths: 3, by_operation: { modify: 6 } },
        { directory: "/etc", paths: 1, by_operation: { create: 2 } }],
      directories_truncated: true,
      files: { changed: 3, outside_project: 1, temporary: 2, agent_files: 40 },
      domains: [{ domain: "github.com", lookups: 2, connections: 3, new: false },
        { domain: "anthropic.com", lookups: 1, connections: 0, new: false },
        { domain: "evil.test", lookups: 1, connections: 0, new: true }],
      timeline: [4, 0, 1],
      command_timeline: [0, 2, 0],
      timeline_start_ns: 0,
      timeline_bucket_ns: 20 * MINUTE_NS,
    }),
  });
}

function host(hostname: string): HostDetail {
  return { host_id: "host-1", hostname } as HostDetail;
}

function stoppedRead(): Alert {
  return {
    id: 7, host_id: "host-1", rule_id: "agent_credential_read", source: "detection", severity: "high", title: "AI agent read a credential file",
    description: "An AI agent's /bin/cat was stopped from reading your SSH private key (/Users/dev/.ssh/id_ed25519)",
  } as Alert;
}

function renderSummary(read: AgentSessionTotals | undefined, { onAlerts = vi.fn(), alerts = [] as Alert[] } = {}) {
  render(
    <SessionSummary hostId="host-1" alerts={alerts} totals={read} agent="claude-code 2.1.0" project="/Users/dev/app" firstNs={0}
      lastNs={42 * MINUTE_NS} onAlerts={onAlerts} />,
  );
  return onAlerts;
}

// card is the Summary's card, once the host's name is read: the image it shares, named by its alt text.
async function card(): Promise<HTMLElement> {
  return screen.findByRole("img", { name: /^A summary card of a claude-code 2\.1\.0 session/ });
}

// drawn is the SVG the card's image shows.
function drawn(image: HTMLElement): string {
  return decodeURIComponent(image.getAttribute("src") ?? "");
}

// press clicks one of the share buttons and waits for the panel to say what came of it.
async function press(name: string, says: string) {
  fireEvent.click(screen.getByRole("button", { name }));
  await waitFor(() => { expect(screen.getByText(says)).toBeVisible(); });
}

let clipboard: { write: ReturnType<typeof vi.fn>; writeText: ReturnType<typeof vi.fn> };

beforeEach(() => {
  vi.mocked(sharePng).mockClear();
  vi.spyOn(api, "getHostDetail").mockResolvedValue(host("devs-mbp"));
  clipboard = { write: vi.fn(() => Promise.resolve()), writeText: vi.fn(() => Promise.resolve()) };
  vi.stubGlobal("navigator", Object.defineProperty(Object.create(navigator) as Navigator, "clipboard", { value: clipboard }));
  vi.stubGlobal("ClipboardItem", class { constructor(readonly items: Record<string, Promise<Blob>>) {} });
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("SessionSummary", () => {
  // spec:web-ui/agent-session-summary-tab/the-summary-describes-the-session
  it("shows the session as the card it is shared as: what its log reports beside what ran, what it changed and where it reached", async () => {
    renderSummary(busy(), { alerts: [stoppedRead()] });
    const image = await card();
    expect(image).toBeVisible();
    const svg = drawn(image);
    expect(svg).toContain('width="1080" height="1350"');
    for (const [value, label] of [["12", "tool calls"], ["140", "processes"], ["3", "files changed"], ["3", "domains contacted"]]) {
      expect(labelled(svg, value, label)).toBe(true);
    }
    expect(svg).toContain(">1 outside the project<");
    expect(svg).toContain(">2 temporary files not counted<");
    expect(svg).toContain(">Ran for 42 min · 50k tokens<");
    expect(svg).toContain(">git<");
    expect(svg).toContain(">Git hosts<");
    expect(svg).toContain(">The credential guard stopped it reading my SSH<");
    // The host's name and the person's home folder never reach it.
    expect(svg).not.toMatch(/devs-mbp|\/Users\/dev/);
    expect(screen.getByText(/^Not counted: 40 files Claude Code keeps for itself/)).toBeVisible();
  });

  // spec:web-ui/a-session-can-be-shared-as-an-image-made-in-the-browser/the-summary-shows-the-image-it-shares
  // spec:web-ui/a-session-can-be-shared-as-an-image-made-in-the-browser/the-image-is-copied-or-downloaded-and-the-caption-copied
  it("copies the image it shows as a PNG, downloads it, and copies the caption", async () => {
    const saved: { href: string; download: string }[] = [];
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) {
      saved.push({ href: this.href, download: this.download });
    });
    vi.stubGlobal("URL", Object.assign(URL, { createObjectURL: vi.fn(() => "blob:share"), revokeObjectURL: vi.fn() }));
    renderSummary(busy());
    const image = await card();
    expect(screen.getByText(/^The image is the card shown here, made in your browser\. Nothing is uploaded/)).toBeVisible();

    await press("Copy image", "Image copied.");
    const item = (clipboard.write.mock.calls[0][0] as { items: Record<string, Promise<Blob>> }[])[0];
    expect(Object.keys(item.items)).toEqual(["image/png"]);
    expect((await item.items["image/png"]).type).toBe("image/png");
    // What is copied is drawn from the SVG the Summary shows.
    expect(drawn(image)).toContain(vi.mocked(sharePng).mock.calls[0][0]);

    await press("Download PNG", "Image downloaded.");
    expect(saved).toEqual([{ href: "blob:share", download: "rawtrail-session.png" }]);

    await press("Copy caption", "Caption copied.");
    expect(clipboard.writeText).toHaveBeenCalledWith("My AI agent's log shows 12 tool calls. The kernel saw 140 processes run. "
      + "Recorded with RawTrail.");
    expect(screen.getByRole("link", { name: "Open X" }).getAttribute("href")).toMatch(/^https:\/\/x\.com\/intent\/post\?text=My%20AI/);
    expect(screen.getByRole("link", { name: "Open LinkedIn" }).getAttribute("href")).toMatch(/^https:\/\/www\.linkedin\.com\/feed/);
  });

  it("says so when the browser cannot copy an image", async () => {
    vi.stubGlobal("ClipboardItem", undefined);
    renderSummary(busy());
    await card();
    await press("Copy image", "This browser cannot copy an image. Download it instead.");
  });

  // spec:web-ui/a-session-can-be-shared-as-an-image-made-in-the-browser/a-host-whose-name-cannot-be-read-is-not-shared
  it.each([
    ["cannot be read", () => Promise.reject(new Error("boom"))],
    ["is empty", () => Promise.resolve(host("  "))],
  ])("shows no card and offers nothing to share when the host's name %s", async (_name, read) => {
    vi.spyOn(api, "getHostDetail").mockImplementation(read);
    renderSummary(busy());
    expect(await screen.findByRole("alert")).toHaveTextContent("This host's name could not be read");
    // A shown image can be copied from the page whatever the buttons offer, so one that cannot be checked is not drawn.
    expect(screen.queryByRole("img")).toBeNull();
    // Nor is a card left loading that will never be drawn.
    expect(document.querySelector("[aria-busy='true']")).toBeNull();
    for (const name of ["Copy image", "Download PNG", "Copy caption"]) expect(screen.getByRole("button", { name })).toBeDisabled();
    expect(screen.queryByRole("link", { name: "Open LinkedIn" })).toBeNull();
  });

  it("draws no card for another host until that host's name is read, never redacting against the last one", async () => {
    vi.spyOn(api, "getHostDetail").mockImplementation((id) => id === "host-1" ? Promise.resolve(host("devs-mbp"))
      : new Promise(() => undefined));
    const summary = (hostId: string) => (
      <SessionSummary hostId={hostId} alerts={[]} totals={busy()} agent="claude-code 2.1.0" project="/Users/dev/app" firstNs={0}
        lastNs={42 * MINUTE_NS} onAlerts={vi.fn()} />
    );
    const { rerender } = render(summary("host-1"));
    await card();
    rerender(summary("host-2"));
    expect(screen.queryByRole("img")).toBeNull();
    expect(screen.getByRole("button", { name: "Copy image" })).toBeDisabled();
  });

  it("counts its alerts and links to where they are shown, without leading with them", async () => {
    const onAlerts = renderSummary(busy());
    await card();
    expect(screen.getByText(/^2 alerts were raised in this session\./)).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "See them in the Timeline" }));
    expect(onAlerts).toHaveBeenCalledOnce();
  });

  // spec:web-ui/agent-session-summary-tab/an-empty-session-reads-as-empty
  it("draws an empty session's card with nothing in it, and says no alert was raised", async () => {
    renderSummary(totals());
    const svg = drawn(await card());
    for (const label of ["tool calls", "processes", "files changed", "domains contacted"]) {
      expect(labelled(svg, "0", label)).toBe(true);
    }
    expect(svg).not.toContain("Programs it ran most");
    expect(svg).not.toContain("Where it talked to");
    expect(screen.getByText("No alerts were raised in this session.")).toBeVisible();
    expect(screen.queryByText(/^Not counted/)).toBeNull();
  });

  it("says when the activity reflects only part of the session", async () => {
    renderSummary({ ...busy(), scope: { applied: true, processes: 4, unmatched: 0, truncated: true } });
    await card();
    expect(screen.getByText("This scope has more processes than are read, so its activity is partial.")).toBeVisible();
  });

  it("says nothing about scope when the activity reflects the whole session", async () => {
    renderSummary({ ...busy(), scope: { applied: true, processes: 4, unmatched: 0, truncated: false } });
    await card();
    expect(screen.queryByText(/its activity is partial/)).toBeNull();
  });

  it("says when the summary could not be read", () => {
    renderSummary(undefined);
    expect(screen.getByText("The session's summary could not be read.")).toBeVisible();
  });
});
