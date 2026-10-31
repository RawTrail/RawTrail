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

  it("cuts a word longer than a line", () => {
    expect(wrap("supercalifragilistic", 50, 200, 1)).toEqual(["superc…"]);
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
