import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import * as api from "../../api";
import type { AgentSessionDigest, AgentSessionTotals, HostDetail } from "../../types";
import { ShareDialog } from "./ShareDialog";
import { sharePng } from "./shareImage";

vi.mock("./shareImage", async (original) => ({
  ...await original<typeof import("./shareImage")>(),
  sharePng: vi.fn(() => Promise.resolve(new Blob(["png"], { type: "image/png" }))),
}));

const MINUTE_NS = 60e9;

function digest(over: Partial<AgentSessionDigest> = {}): AgentSessionDigest {
  return {
    prompts: 3, programs: [{ program: "/usr/bin/git", count: 7 }],
    directories: [{ directory: "/Users/ann/app/src", paths: 2, by_operation: {} }],
    directories_truncated: false, files: { changed: 0, outside_project: 0, temporary: 0, agent_files: 0 },
    domains: [{ domain: "github.com", lookups: 1, connections: 1, new: false },
      { domain: "anns-mbp.local", lookups: 1, connections: 0, new: false }],
    domains_truncated: false, timeline: [1, 2], command_timeline: [1, 2], timeline_start_ns: 0, timeline_bucket_ns: MINUTE_NS, ...over,
  };
}

const TOTALS: AgentSessionTotals = {
  commands: 12, processes: 30, alerts: { total: 0, by_severity: {} }, tool_calls: { total: 40, by_tool: [] },
  activity: { files: { events: 5, distinct: 3, by_operation: {} }, network: { events: 1, distinct: 1 }, dns: { events: 2, distinct: 2 } },
  digest: digest(),
};

function host(hostname: string): HostDetail {
  return { host_id: "h1", hostname } as HostDetail;
}

function renderDialog(onClose = vi.fn()) {
  render(<ShareDialog hostId="h1" totals={TOTALS} alerts={[]} agent="Claude Code" project="/Users/ann/app" firstNs={0}
    lastNs={20 * MINUTE_NS} onClose={onClose} />);
  return onClose;
}

// press clicks one of the dialog's buttons and waits for the dialog to say what came of it.
async function press(name: string, says: string) {
  fireEvent.click(screen.getByRole("button", { name }));
  await waitFor(() => { expect(screen.getByRole("status")).toHaveTextContent(says); });
}

let clipboard: { write: ReturnType<typeof vi.fn>; writeText: ReturnType<typeof vi.fn> };

beforeEach(() => {
  vi.mocked(sharePng).mockClear();
  HTMLDialogElement.prototype.showModal = function showModal() { this.open = true; };
  clipboard = { write: vi.fn(() => Promise.resolve()), writeText: vi.fn(() => Promise.resolve()) };
  vi.stubGlobal("navigator", Object.defineProperty(Object.create(navigator) as Navigator, "clipboard", { value: clipboard }));
  vi.stubGlobal("ClipboardItem", class { constructor(readonly items: Record<string, Promise<Blob>>) {} });
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("ShareDialog", () => {
  // spec:web-ui/a-session-can-be-shared-as-an-image-made-in-the-browser/the-preview-is-the-exported-image-and-choices-change-it
  it("previews the redacted card and redraws it as the person chooses", async () => {
    vi.spyOn(api, "getHostDetail").mockResolvedValue(host("anns-mbp"));
    renderDialog();
    const preview = await screen.findByRole("img", { name: /^A summary card of a Claude Code session/ });
    expect(preview).toBeVisible();
    expect(screen.getByText("The image is made in your browser. Nothing is uploaded, and there are no share links.")).toBeVisible();
    // The host's own name is not among the destinations, and the project is hidden until chosen.
    expect(preview.getAttribute("alt")).not.toMatch(/anns|ann\b/);
    expect(preview.getAttribute("alt")).not.toContain(" in app");
    const portrait = preview.getAttribute("src");
    expect(decodeURIComponent(portrait ?? "")).toContain('width="1080" height="1350"');

    fireEvent.click(screen.getByRole("switch", { name: "Show the project name" }));
    expect(screen.getByRole("img", { name: /session in app/ })).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: /^Landscape/ }));
    expect(screen.getByRole("button", { name: /^Landscape/ })).toHaveAttribute("aria-pressed", "true");
    expect(decodeURIComponent(screen.getByRole("img", { name: /session in app/ }).getAttribute("src") ?? ""))
      .toContain('width="1200" height="675"');
  });

  it("shows the edited prompt as the title, redacted", async () => {
    vi.spyOn(api, "getHostDetail").mockResolvedValue(host("anns-mbp"));
    renderDialog();
    await screen.findByRole("img", { name: /summary card/ });
    fireEvent.click(screen.getByRole("switch", { name: "Show a prompt as the title" }));
    fireEvent.change(screen.getByLabelText(/Title/), { target: { value: "Deploy from anns-mbp tonight" } });
    expect(screen.getByRole("img", { name: /Deploy from … tonight\./ })).toBeVisible();
    // Turned off, the title goes, though the text typed is kept for turning it on again.
    fireEvent.click(screen.getByRole("switch", { name: "Show a prompt as the title" }));
    expect(screen.getByRole("img", { name: /: What my AI agent did in 20 min\./ })).toBeVisible();
  });

  // spec:web-ui/a-session-can-be-shared-as-an-image-made-in-the-browser/the-image-is-copied-or-downloaded-and-the-caption-copied
  it("copies the image as a PNG, downloads it, and copies the caption", async () => {
    vi.spyOn(api, "getHostDetail").mockResolvedValue(host("anns-mbp"));
    const saved: { href: string; download: string }[] = [];
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) {
      saved.push({ href: this.href, download: this.download });
    });
    vi.stubGlobal("URL", Object.assign(URL, { createObjectURL: vi.fn(() => "blob:share"), revokeObjectURL: vi.fn() }));
    renderDialog();
    await screen.findByRole("img", { name: /summary card/ });

    await press("Copy image", "Image copied.");
    const item = (clipboard.write.mock.calls[0][0] as { items: Record<string, Promise<Blob>> }[])[0];
    expect(Object.keys(item.items)).toEqual(["image/png"]);
    expect((await item.items["image/png"]).type).toBe("image/png");

    await press("Download PNG", "Image downloaded.");
    expect(click).toHaveBeenCalledOnce();
    expect(saved).toEqual([{ href: "blob:share", download: "rawtrail-session.png" }]);

    // The image exported is the size previewed.
    fireEvent.click(screen.getByRole("button", { name: /^Landscape/ }));
    await press("Copy image", "Image copied.");
    const drawn = vi.mocked(sharePng).mock.calls;
    expect(drawn[0][1]).toBe("portrait");
    expect(drawn[drawn.length - 1][1]).toBe("landscape");
    expect(decodeURIComponent(screen.getByRole("img", { name: /summary card/ }).getAttribute("src") ?? ""))
      .toContain(drawn[drawn.length - 1][0]);

    await press("Copy caption", "Caption copied.");
    expect(clipboard.writeText).toHaveBeenCalledWith(expect.stringMatching(/^20 min, 12 commands and 40 tool calls: /));
    expect(screen.getByRole("link", { name: "Open X" }).getAttribute("href")).toMatch(/^https:\/\/x\.com\/intent\/post\?text=20%20min/);
  });

  it("says so when the browser cannot copy an image", async () => {
    vi.spyOn(api, "getHostDetail").mockResolvedValue(host("anns-mbp"));
    vi.stubGlobal("ClipboardItem", undefined);
    renderDialog();
    await screen.findByRole("img", { name: /summary card/ });
    await press("Copy image", "This browser cannot copy an image. Download it instead.");
  });

  // spec:web-ui/a-session-can-be-shared-as-an-image-made-in-the-browser/a-host-whose-name-cannot-be-read-is-not-shared
  it("shares nothing when the host's name cannot be read", async () => {
    vi.spyOn(api, "getHostDetail").mockRejectedValue(new Error("boom"));
    renderDialog();
    expect(await screen.findByRole("alert")).toHaveTextContent("This host's name could not be read");
    expect(screen.queryByRole("img")).toBeNull();
    for (const name of ["Copy image", "Download PNG", "Copy caption"]) expect(screen.getByRole("button", { name })).toBeDisabled();
    expect(screen.queryByRole("link", { name: "Open LinkedIn" })).toBeNull();
  });

  it("shares nothing for a host with no enrolled name", async () => {
    vi.spyOn(api, "getHostDetail").mockResolvedValue(host("  "));
    renderDialog();
    expect(await screen.findByRole("alert")).toHaveTextContent("This host's name could not be read");
    expect(screen.queryByRole("img")).toBeNull();
    expect(screen.getByRole("button", { name: "Copy image" })).toBeDisabled();
  });

  // spec:web-ui/a-session-can-be-shared-as-an-image-made-in-the-browser/a-title-is-checked-for-the-user-s-name-or-the-person-is-warned
  it.each([
    ["a program's home folder", "/Users/ann/bin/deploy", "Help ann fix it", "Help … fix it"],
    ["the title's own home folder", "/usr/bin/git", "Help ann fix /Users/ann/app", "Help … fix ~/app"],
  ])("finds the username in %s when the project names none", async (_name, program, typed, shown) => {
    vi.spyOn(api, "getHostDetail").mockResolvedValue(host("build-01"));
    const totals = { ...TOTALS, digest: digest({ directories: [], programs: [{ program, count: 2 }] }) };
    render(<ShareDialog hostId="h1" totals={totals} alerts={[]} agent="Claude Code" project="/srv/app" firstNs={0}
      lastNs={20 * MINUTE_NS} onClose={vi.fn()} />);
    await screen.findByRole("img", { name: /summary card/ });
    fireEvent.click(screen.getByRole("switch", { name: "Show a prompt as the title" }));
    fireEvent.change(screen.getByLabelText(/Title/), { target: { value: typed } });
    expect(screen.getByRole("img", { name: (accessible) => accessible.includes(`: ${shown}.`) })).toBeVisible();
    expect(screen.queryByText(/names no home folder/)).toBeNull();
  });

  it("warns that a username in the title cannot be recognized when no home folder names one", async () => {
    vi.spyOn(api, "getHostDetail").mockResolvedValue(host("build-01"));
    const totals = { ...TOTALS, digest: digest({ directories: [], programs: [{ program: "/usr/bin/git", count: 2 }] }) };
    render(<ShareDialog hostId="h1" totals={totals} alerts={[]} agent="Claude Code" project="/srv/app" firstNs={0}
      lastNs={20 * MINUTE_NS} onClose={vi.fn()} />);
    await screen.findByRole("img", { name: /summary card/ });
    expect(screen.queryByText(/names no home folder/)).toBeNull();
    fireEvent.click(screen.getByRole("switch", { name: "Show a prompt as the title" }));
    expect(screen.getByText(/names no home folder, so a username in the title cannot be recognized/)).toBeVisible();
  });

  it("draws the project's name into the image when the person shows it", async () => {
    vi.spyOn(api, "getHostDetail").mockResolvedValue(host("anns-mbp"));
    renderDialog();
    const image = () => decodeURIComponent(screen.getByRole("img", { name: /summary card/ }).getAttribute("src") ?? "");
    await screen.findByRole("img", { name: /summary card/ });
    expect(image()).not.toContain("SESSION · APP");
    fireEvent.click(screen.getByRole("switch", { name: "Show the project name" }));
    expect(image()).toContain("SESSION · APP");
  });

  it("closes on Escape and on a click on its backdrop, with the latest onClose", async () => {
    vi.spyOn(api, "getHostDetail").mockResolvedValue(host("anns-mbp"));
    const first = vi.fn();
    const latest = vi.fn();
    const props = { hostId: "h1", totals: TOTALS, alerts: [], agent: "Claude Code", project: "/Users/ann/app", firstNs: 0,
      lastNs: 20 * MINUTE_NS };
    const { rerender } = render(<ShareDialog {...props} onClose={first} />);
    await screen.findByRole("img", { name: /summary card/ });
    rerender(<ShareDialog {...props} onClose={latest} />);
    const dialog = screen.getByRole("dialog");
    fireEvent(dialog, new Event("cancel", { cancelable: true }));
    fireEvent.click(dialog);
    expect(latest).toHaveBeenCalledTimes(2);
    expect(first).not.toHaveBeenCalled();
  });

  it("closes from its Close button", async () => {
    vi.spyOn(api, "getHostDetail").mockResolvedValue(host("anns-mbp"));
    const onClose = renderDialog();
    await screen.findByRole("img", { name: /summary card/ });
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalledOnce();
  });
});
