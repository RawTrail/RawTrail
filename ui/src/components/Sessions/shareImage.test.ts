import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ShareCard } from "./shareCard";
import { SCALE, sharePng, shareSvg, SIZE, svgDataUrl, wrap } from "./shareImage";
import { labelled, texts } from "../../test/svgText";

function card(over: Partial<ShareCard> = {}): ShareCard {
  return {
    agent: "Claude Code", date: "Oct 30, 2026", duration: "47 min", tokens: "3.1M", toolCalls: 1286, processes: 903, filesChanged: 41,
    outsideProject: 4, temporary: 12, domains: 3, domainsCut: false,
    programs: [{ name: "go", count: 132 }, { name: "git", count: 61 }],
    destinations: [{ label: "Model APIs", count: 1, domains: ["api.anthropic.com"] }],
    guard: { credentials: ["my AWS credentials"] }, ...over,
  };
}

// jsdom has no HTMLImageElement.decode; each test that draws gives it one.
const decode = vi.fn<() => Promise<void>>();
beforeEach(() => {
  decode.mockReset().mockResolvedValue(undefined);
  Object.defineProperty(HTMLImageElement.prototype, "decode", { configurable: true, value: decode });
});
afterEach(() => { vi.restoreAllMocks(); });

describe("wrap", () => {
  it("keeps text that fits on one line", () => {
    expect(wrap("Add retry with backoff", 64, 920, 3)).toEqual(["Add retry with backoff"]);
  });

  it("breaks at words and ends an overflowing last line with an ellipsis", () => {
    const lines = wrap("one two three four five six seven eight nine ten", 50, 200, 2);
    expect(lines).toHaveLength(2);
    expect(lines[1].endsWith("…")).toBe(true);
    for (const line of lines) expect(line.length).toBeLessThanOrEqual(7);
  });

  it("breaks a word longer than a line across lines, and cuts it when out of lines", () => {
    expect(wrap("supercalifragilistic", 50, 200, 1)).toEqual(["superc…"]);
    expect(wrap("supercalifragilistic", 50, 200, 4)).toEqual(["superca", "lifragi", "listic"]);
  });

  it("counts a CJK character or an emoji a full em wide", () => {
    const lines = wrap("漢".repeat(50), 64, 920, 3);
    // 920 / 64 is 14.4 ems: 14 characters a line; the third is cut to 13 and its ellipsis.
    expect(lines.map((l) => Array.from(l).length)).toEqual([14, 14, 14]);
    expect(lines[2].endsWith("…")).toBe(true);
  });

  it("never splits an emoji, so every line can be encoded", () => {
    for (const lines of [wrap("abcdefghijklmnopqrstuvw😀tail", 64, 920, 3), wrap("😀".repeat(30), 64, 920, 1)]) {
      for (const line of lines) expect(() => encodeURIComponent(line)).not.toThrow();
    }
    expect(() => svgDataUrl(shareSvg(card({ agent: `${"x".repeat(23)}😀${"y".repeat(60)}` })))).not.toThrow();
  });
});

describe("shareSvg", () => {
  it("draws the portrait card with its headline numbers, what ran in the accent", () => {
    const svg = shareSvg(card());
    expect(svg.startsWith('<svg xmlns="http://www.w3.org/2000/svg" width="1080" height="1350" ')).toBe(true);
    expect(svg).toContain(">What my AI agent did<");
    expect(svg).toContain(">Ran for 47 min · 3.1M tokens<");
    expect(svg).toContain(">CLAUDE CODE SESSION<");
    expect(svg).toContain(">Oct 30, 2026<");
    for (const [value, label] of [["1,286", "tool calls"], ["903", "processes"], ["41", "files changed"], ["3", "domains contacted"]]) {
      expect(labelled(svg, value, label)).toBe(true);
    }
    expect(svg).toMatch(/fill="#3fd6b4"[^>]*>903</);
    expect(svg).toContain(">in the agent&#39;s own log<");
    expect(svg).toContain(">actually ran<");
    expect(svg).toContain(">4 outside the project<");
    expect(svg).toContain(">12 temporary files not counted<");
    expect(svg).toContain(">Recorded with RawTrail<");
    expect(svg).toContain(">github.com/RawTrail/RawTrail<");
  });

  it("says one of each in the singular, and a cut domain list as at least its length", () => {
    const svg = shareSvg(card({ toolCalls: 1, processes: 1, filesChanged: 1, domains: 1 }));
    for (const label of ["tool call", "process", "file changed", "domain contacted"]) expect(svg).toContain(`>${label}<`);
    expect(labelled(shareSvg(card({ domains: 50, domainsCut: true })), "50+", "domains contacted")).toBe(true);
  });

  it("leaves out what the session does not have: its length, tokens, project and temporary files", () => {
    const svg = shareSvg(card({ duration: null, tokens: null, date: null, outsideProject: null, temporary: 0 }));
    expect(svg).not.toContain("Ran for");
    expect(svg).not.toContain("tokens");
    expect(svg).not.toContain("outside the project");
    expect(svg).not.toContain("temporary");
    expect(shareSvg(card({ duration: null }))).toContain(">3.1M tokens<");
  });

  it("escapes every string, so text on the card cannot become markup", () => {
    const svg = shareSvg(card({ agent: `<b>"x"</b> & 'y'`, programs: [{ name: "<img>", count: 1 }] }));
    expect(svg).not.toContain("<b>");
    expect(svg).not.toContain("<img>");
    expect(svg).toContain("&lt;B&gt;&quot;X&quot;&lt;/B&gt; &amp; &#39;Y&#39;");
  });

  it("shows what the credential guard stopped only when the card has it", () => {
    expect(shareSvg(card())).toContain(">The credential guard stopped it reading my AWS<");
    expect(shareSvg(card({ guard: null }))).not.toContain("credential guard");
    // Three credentials read as the caption words them, "A, B and C", up to where the line is cut to the card's width.
    const three = card({ guard: { credentials: ["my SSH private key", "my AWS credentials", "my GitHub CLI token"] } });
    expect(texts(shareSvg(three)).join(" ")).toContain("reading my SSH private key, my AWS credentials and my GitHub");
  });

  it("never draws over the footer: a full card shows its groups without their domains", () => {
    const groups = ["Model APIs", "Git hosts", "Package registries", "Everything else"].map((label) => ({ label, count: 1,
      domains: [`${label.split(" ")[0].toLowerCase()}.example.com`] }));
    const programs = ["go", "git", "rg", "npm"].map((name) => ({ name, count: 1 }));
    const full = shareSvg(card({ destinations: groups, programs }));
    const guard = /<rect x="80\.0" y="([\d.]+)" width="920\.0" height="([\d.]+)" rx="16"/.exec(full);
    expect(guard).not.toBeNull();
    const footerTop = SIZE.height - 80 - 24;
    expect(Number(guard?.[1]) + Number(guard?.[2])).toBeLessThan(footerTop);
    expect(full).not.toContain("model.example.com");
    // With room to spare, the same groups name their domains.
    expect(shareSvg(card({ destinations: groups, programs, guard: null }))).toContain("model.example.com");
  });

  it("leaves out programs and destinations a session does not have", () => {
    const svg = shareSvg(card({ programs: [], destinations: [], guard: null }));
    expect(svg).not.toContain("Programs it ran most");
    expect(svg).not.toContain("Where it talked to");
  });
});

// spec:web-ui/a-session-can-be-shared-as-an-image-made-in-the-browser/the-image-is-drawn-in-the-browser-without-a-request
describe("sharePng", () => {
  it("draws the SVG from a data URL onto a canvas twice the card's size and saves a PNG", async () => {
    const drawImage = vi.fn();
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue({ drawImage } as unknown as CanvasRenderingContext2D);
    const toBlob = vi.spyOn(HTMLCanvasElement.prototype, "toBlob").mockImplementation(function (this: HTMLCanvasElement, done, type) {
      done(new Blob([`${String(this.width)}x${String(this.height)}`], { type }));
    });
    const svg = shareSvg(card());

    const png = await sharePng(svg);

    expect(decode).toHaveBeenCalledOnce();
    expect((drawImage.mock.calls[0][0] as HTMLImageElement).src).toBe(svgDataUrl(svg));
    expect(drawImage.mock.calls[0].slice(1)).toEqual([0, 0, SIZE.width * SCALE, SIZE.height * SCALE]);
    expect(toBlob.mock.calls[0][1]).toBe("image/png");
    expect(png.type).toBe("image/png");
    expect(await png.text()).toBe("2160x2700");
  });

  it("fails plainly in a browser that cannot draw", async () => {
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
    await expect(sharePng(shareSvg(card()))).rejects.toThrow("cannot draw the image");
  });

  it("keeps the SVG inside the URL, so showing it makes no request", () => {
    expect(svgDataUrl("<svg/>")).toBe("data:image/svg+xml;charset=utf-8,%3Csvg%2F%3E");
  });
});
