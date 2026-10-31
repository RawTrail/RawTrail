import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ShareCard } from "./shareCard";
import { SCALE, sharePng, shareSvg, SIZES, svgDataUrl, wrap } from "./shareImage";

function card(over: Partial<ShareCard> = {}): ShareCard {
  return {
    agent: "Claude Code", date: "Oct 30, 2026", title: null, project: null, duration: "47 min", prompts: 6, commands: 412, toolCalls: 1286,
    tokens: "3.1M", timeline: [1, 0, 4, 2], timelineStart: "4:14 PM", timelineEnd: "5:02 PM",
    programs: [{ name: "go", count: 132 }, { name: "git", count: 61 }],
    destinations: [{ label: "Model APIs", count: 1, domains: ["api.anthropic.com"] }],
    fileEvents: 312, paths: 48, outsideProject: 0, guard: { credentials: ["my AWS credentials"] }, ...over,
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
    expect(() => svgDataUrl(shareSvg(card({ title: `${"x".repeat(23)}😀${"y".repeat(60)}` }), "portrait"))).not.toThrow();
  });
});

describe("shareSvg", () => {
  it.each([["portrait", 1080, 1350], ["landscape", 1200, 675]] as const)("draws the %s card at %ix%i", (size, width, height) => {
    const svg = shareSvg(card(), size);
    expect(svg.startsWith(`<svg xmlns="http://www.w3.org/2000/svg" width="${String(width)}" height="${String(height)}" `)).toBe(true);
    expect(svg).toContain(">Recorded with RawTrail<");
    expect(svg).toContain(">github.com/RawTrail/RawTrail<");
    expect(svg).toContain(">412<");
  });

  it("shows the person's title, or says what the agent did when there is none", () => {
    expect(shareSvg(card({ title: "Fix the flaky test" }), "portrait")).toContain(">Fix the flaky test<");
    // A long line wraps, so the headline is checked across its two lines.
    expect(shareSvg(card(), "portrait")).toMatch(/>What my AI agent did in<\/text><text [^>]*>47 min</);
  });

  it("escapes every string, so text on the card cannot become markup", () => {
    const svg = shareSvg(card({ title: `<b>"x"</b> & 'y'`, programs: [{ name: "<img>", count: 1 }] }), "portrait");
    expect(svg).not.toContain("<b>");
    expect(svg).not.toContain("<img>");
    expect(svg).toContain(">&lt;b&gt;&quot;x&quot;&lt;/b&gt; &amp; &#39;y&#39;<");
  });

  it("shows what the credential guard stopped only when the card has it", () => {
    expect(shareSvg(card(), "portrait")).toContain(">The credential guard stopped it reading my AWS<");
    expect(shareSvg(card(), "landscape")).toContain("Stopped it reading my AWS");
    expect(shareSvg(card({ guard: null }), "portrait")).not.toContain("credential guard");
  });

  it("names the project in the heading when the person chose it, and each group's domains", () => {
    for (const size of ["portrait", "landscape"] as const) {
      const svg = shareSvg(card({ project: "webhooks" }), size);
      expect(svg).toContain("CLAUDE CODE SESSION · WEBHOOKS");
      expect(svg).toContain("api.anthropic.com");
      expect(shareSvg(card(), size)).not.toContain("WEBHOOKS");
    }
  });

  it("never draws over the footer: a full card shows its groups without their domains", () => {
    const groups = ["Model APIs", "Git hosts", "Package registries", "Everything else"].map((label) => ({ label, count: 1,
      domains: [`${label.split(" ")[0].toLowerCase()}.example.com`] }));
    const programs = ["go", "git", "rg", "npm"].map((name) => ({ name, count: 1 }));
    const full = shareSvg(card({ title: "x ".repeat(80), destinations: groups, programs }), "portrait");
    const guard = /<rect x="80\.0" y="([\d.]+)" width="920\.0" height="([\d.]+)" rx="16"/.exec(full);
    expect(guard).not.toBeNull();
    const footerTop = SIZES.portrait.height - 80 - 24;
    expect(Number(guard?.[1]) + Number(guard?.[2])).toBeLessThan(footerTop);
    expect(full).not.toContain("model.example.com");
    // With room to spare, the same groups name their domains.
    expect(shareSvg(card({ destinations: groups, programs, guard: null }), "portrait")).toContain("model.example.com");
  });

  it("leaves out a timeline, programs and destinations a session does not have", () => {
    const svg = shareSvg(card({ timeline: [], programs: [], destinations: [], guard: null }), "portrait");
    expect(svg).not.toContain("When it was busy");
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
    const svg = shareSvg(card(), "landscape");

    const png = await sharePng(svg, "landscape");

    expect(decode).toHaveBeenCalledOnce();
    expect((drawImage.mock.calls[0][0] as HTMLImageElement).src).toBe(svgDataUrl(svg));
    expect(drawImage.mock.calls[0].slice(1)).toEqual([0, 0, SIZES.landscape.width * SCALE, SIZES.landscape.height * SCALE]);
    expect(toBlob.mock.calls[0][1]).toBe("image/png");
    expect(png.type).toBe("image/png");
    expect(await png.text()).toBe("2400x1350");
  });

  it("fails plainly in a browser that cannot draw", async () => {
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
    await expect(sharePng(shareSvg(card(), "portrait"), "portrait")).rejects.toThrow("cannot draw the image");
  });

  it("keeps the SVG inside the URL, so showing it makes no request", () => {
    expect(svgDataUrl("<svg/>")).toBe("data:image/svg+xml;charset=utf-8,%3Csvg%2F%3E");
  });
});
