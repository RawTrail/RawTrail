// The image a session is shared as (issue #1430): an SVG drawn from a share card alone, and the PNG made from it in the browser.
// Nothing here reads anything but the card, so nothing the card leaves out can reach the image, and no request is made: the text uses
// the system's fonts.

import { headline, type ShareCard } from "./shareCard";

export type ShareSize = "portrait" | "landscape";

// SIZES are the two shapes a card is drawn in: 4:5 portrait, which a feed shows largest, and 16:9 landscape, which X and link
// previews show uncropped.
export const SIZES: Readonly<Record<ShareSize, { width: number; height: number; label: string }>> = {
  portrait: { width: 1080, height: 1350, label: "Portrait 1080 x 1350, for LinkedIn" },
  landscape: { width: 1200, height: 675, label: "Landscape 1200 x 675, for X" },
};

// SCALE is how many image pixels each card pixel becomes: 2, so the image stays sharp on a high-density screen.
export const SCALE = 2;

const PROJECT_URL = "github.com/RawTrail/RawTrail";
const FONT = "-apple-system, BlinkMacSystemFont, 'Segoe UI', Inter, Helvetica, Arial, sans-serif";
const MONO = "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace";
const COLORS = { ground: "#141a3a", text: "#ffffff", muted: "#b3b8d6", faint: "#8b91b8", accent: "#3fd6b4", panel: "#1d2550",
  rule: "#2c3463" };
// GLYPH and WIDE_GLYPH are a glyph's width as a share of its font size, for wrapping text the SVG cannot wrap itself: an average Latin
// glyph, leaning wide so a line ends early rather than running off the card, and a CJK character or emoji, which takes a full em.
const GLYPH = 0.56;
const WIDE_GLYPH = 1;
// WIDE are the code point ranges drawn a full em wide: Hangul, CJK, fullwidth forms, and emoji and other symbols above U+1F000.
const WIDE: readonly { lo: number; hi: number }[] = [{ lo: 0x1100, hi: 0x115f }, { lo: 0x2e80, hi: 0xa4cf }, { lo: 0xac00, hi: 0xd7a3 },
  { lo: 0xf900, hi: 0xfaff }, { lo: 0xfe30, hi: 0xfe4f }, { lo: 0xff00, hi: 0xff60 }, { lo: 0xffe0, hi: 0xffe6 },
  { lo: 0x1f000, hi: 0x10ffff }];
const STAT_COLUMNS = 4;
const TITLE_LINES = 3;
const GUARD_LINES = 2;
// BAR is the timeline's bars: the gap between them, the least height a bucket with anything in it shows, and their corner radius.
const BAR = { gap: 6, least: 4, radius: 3 };

// Each size's measurements, in card pixels. Text sizes are what a phone-width feed still reads: 22 px and up on a 1080-wide card.
const PORTRAIT = {
  pad: 80, label: 26, labelGap: 52, title: 64, titleLine: 72, titleBaseline: 14, afterTitle: 52, stat: 58, statLabelGap: 40,
  afterStats: 156, strip: 110, stripGap: 20, axis: 24, axisGap: 62, afterStrip: 232, row: 30, rowGap: 46, listGap: 50, columnGap: 48,
  afterList: 70, detail: 22, detailGap: 30, dateWidth: 220, footerRoom: 60, guardText: 30, guardLine: 40, guardPad: 48, guardIcon: 44,
  guardInset: 28,
  guardTextInset: 96, guardRadius: 16,
  guardBaseline: 8, footer: 24,
} as const;
const LANDSCAPE = {
  pad: 64, top: 60, label: 22, labelGap: 30, title: 46, titleLine: 52, titleBaseline: 10, afterTitle: 40, stat: 52, statLabelGap: 32,
  statRow: 104, columnGap: 56, strip: 110, stripGap: 16, afterStrip: 170, row: 24, rowGap: 42, rowLine: 32, afterRow: 84, afterGroups: 52,
  guardPad: 40, guardLine: 28, guardIcon: 34, guardInset: 18, guardTextInset: 66, guardBaseline: 8, guardRadius: 14, footer: 22,
  footerBottom: 44,
} as const;
// MARK is the footer's square: how much larger than the text it is, its corner radius, and the gap before the text.
const MARK = { grow: 4, radius: 6, gap: 18 };
// ICON_BOX is the edge of the box the shield's path is drawn in.
const ICON_BOX = 24;

function escape(text: string): string {
  return text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;").replace(/'/g, "&#39;");
}

// CAPS_WIDTH is how much wider a spaced capitals label draws than the same text in lower case, for fitting a heading.
const CAPS_WIDTH = 1.3;

// ems is how wide text draws, in ems, counting each code point once so an emoji's two UTF-16 halves are one glyph.
function ems(text: string): number {
  let total = 0;
  for (const ch of text) {
    const cp = ch.codePointAt(0) ?? 0;
    total += WIDE.some(({ lo, hi }) => cp >= lo && cp <= hi) ? WIDE_GLYPH : GLYPH;
  }
  return total;
}

// fit is the longest start of text, whole code points, that fits within limit ems.
function fit(text: string, limit: number): string {
  let out = "";
  for (const ch of text) {
    if (ems(out + ch) > limit) break;
    out += ch;
  }
  return out;
}

// wrap breaks text into at most maxLines lines that fit width at size, at spaces where it can and inside a word too long for a line
// (as CJK text is), ending the last line with an ellipsis when the text runs over. It never splits a code point.
export function wrap(text: string, size: number, width: number, maxLines: number): string[] {
  const limit = width / size;
  const lines: string[] = [];
  let line = "";
  for (let word of text.split(/\s+/).filter((w) => w !== "")) {
    const next = line === "" ? word : `${line} ${word}`;
    if (ems(next) <= limit) {
      line = next;
      continue;
    }
    if (line !== "") lines.push(line);
    while (ems(word) > limit) {
      const head = fit(word, limit) || Array.from(word)[0];
      lines.push(head);
      word = word.slice(head.length);
    }
    line = word;
  }
  if (line !== "") lines.push(line);
  if (lines.length <= maxLines) return lines;
  const kept = lines.slice(0, maxLines);
  kept[maxLines - 1] = `${fit(kept[maxLines - 1], limit - GLYPH)}…`;
  return kept;
}

interface Text {
  readonly x: number;
  readonly y: number;
  readonly text: string;
  readonly size: number;
  readonly color?: string;
  readonly bold?: boolean;
  readonly end?: boolean;
  readonly mono?: boolean;
  readonly caps?: boolean;
}

function text(t: Text): string {
  const attrs = [`x="${String(t.x)}"`, `y="${String(t.y)}"`, `font-size="${String(t.size)}"`, `fill="${t.color ?? COLORS.text}"`,
    `font-family="${t.mono ? MONO : FONT}"`];
  if (t.bold) attrs.push(`font-weight="700"`);
  if (t.end) attrs.push(`text-anchor="end"`);
  if (t.caps) attrs.push(`letter-spacing="2"`);
  return `<text ${attrs.join(" ")}>${escape(t.caps ? t.text.toUpperCase() : t.text)}</text>`;
}

function rect(x: number, y: number, width: number, height: number, fill: string, radius: number): string {
  return `<rect x="${x.toFixed(1)}" y="${y.toFixed(1)}" width="${width.toFixed(1)}" height="${height.toFixed(1)}" rx="${String(radius)}" `
    + `fill="${fill}"/>`;
}

// stats are the card's headline numbers, the ones it has, up to four.
function stats(card: ShareCard): { value: string; label: string }[] {
  const all = [
    card.duration && { value: card.duration, label: "ran for" },
    { value: card.commands.toLocaleString(), label: card.commands === 1 ? "command" : "commands" },
    { value: card.toolCalls.toLocaleString(), label: card.toolCalls === 1 ? "tool call" : "tool calls" },
    card.tokens && { value: card.tokens, label: "tokens" },
    { value: card.prompts.toLocaleString(), label: card.prompts === 1 ? "prompt" : "prompts" },
  ];
  return all.filter((s): s is { value: string; label: string } => Boolean(s)).slice(0, STAT_COLUMNS);
}

// bars draws the timeline's buckets across width, the tallest filling height; a bucket with anything in it shows at least a sliver,
// so a quiet stretch reads as quiet rather than empty.
function bars(timeline: readonly number[], x: number, y: number, width: number, height: number): string {
  const max = Math.max(0, ...timeline);
  if (max === 0) return "";
  const barWidth = (width - BAR.gap * (timeline.length - 1)) / timeline.length;
  return timeline.map((n, i) => {
    const h = n === 0 ? 0 : Math.max(BAR.least, Math.round((n / max) * height));
    return rect(x + i * (barWidth + BAR.gap), y + height - h, barWidth, h, COLORS.accent, BAR.radius);
  }).join("") + rect(x, y + height, width, 2, COLORS.rule, 0);
}

function shield(x: number, y: number, size: number): string {
  return `<g transform="translate(${String(x)} ${String(y)}) scale(${String(size / ICON_BOX)})" fill="none" stroke="${COLORS.accent}" `
    + `stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3l8 3v6c0 4.5-3.4 8.3-8 9-4.6-.7-8-4.5-8-9V6l8-3z"/>`
    + `<path d="M8.5 12l2.5 2.5 4.5-5"/></g>`;
}

// guardLine says what the credential guard stopped, in full on the portrait card and shortened where the landscape card has less room.
function guardLine(card: ShareCard, short = false): string {
  if (!card.guard) return "";
  return `${short ? "Stopped" : "The credential guard stopped"} it reading ${card.guard.credentials.join(" and ")}`;
}

// sessionLabel names the session as the card's heading does: its agent, and its project when the person chose to show it.
function sessionLabel(card: ShareCard): string {
  return card.project ? `${card.agent} session · ${card.project}` : `${card.agent} session`;
}

// footer credits the recording along the card's bottom edge: a mark, "Recorded with RawTrail", and the project's address.
function footer(width: number, pad: number, y: number, size: number): string {
  const mark = size + MARK.grow;
  return rect(pad, y - size + 2, mark, mark, COLORS.accent, MARK.radius)
    + text({ x: pad + mark + MARK.gap, y, text: "Recorded with RawTrail", size, bold: true })
    + text({ x: width - pad, y, text: PROJECT_URL, size, color: COLORS.faint, end: true });
}

function portrait(card: ShareCard): string {
  const L = PORTRAIT;
  const { width, height } = SIZES.portrait;
  const inner = width - 2 * L.pad;
  const parts: string[] = [];
  let y = L.pad + L.label;
  const heading = wrap(sessionLabel(card), L.label, (inner - L.dateWidth) / CAPS_WIDTH, 1)[0] ?? "";
  parts.push(text({ x: L.pad, y, text: heading, size: L.label, color: COLORS.muted, bold: true, caps: true }));
  if (card.date) parts.push(text({ x: width - L.pad, y, text: card.date, size: L.label, color: COLORS.muted, end: true }));
  y += L.labelGap;
  for (const line of wrap(card.title ?? headline(card), L.title, inner, TITLE_LINES)) {
    y += L.titleLine;
    parts.push(text({ x: L.pad, y: y - L.titleBaseline, text: line, size: L.title, bold: true }));
  }
  y += L.afterTitle;
  stats(card).forEach((s, i) => {
    const x = L.pad + i * (inner / STAT_COLUMNS);
    parts.push(
      text({ x, y: y + L.stat, text: s.value, size: L.stat, bold: true, color: i === 0 ? COLORS.accent : COLORS.text }),
      text({ x, y: y + L.stat + L.statLabelGap, text: s.label, size: L.label, color: COLORS.muted }),
    );
  });
  y += L.afterStats;
  if (card.timeline.length > 0) {
    parts.push(
      text({ x: L.pad, y, text: "When it was busy", size: L.label, bold: true, color: COLORS.muted }),
      bars(card.timeline, L.pad, y + L.stripGap, inner, L.strip),
    );
    if (card.timelineStart && card.timelineEnd) {
      const axisY = y + L.stripGap + L.strip + L.axisGap - L.stripGap;
      parts.push(
        text({ x: L.pad, y: axisY, text: card.timelineStart, size: L.axis, color: COLORS.faint }),
        text({ x: width - L.pad, y: axisY, text: card.timelineEnd, size: L.axis, color: COLORS.faint, end: true }),
      );
    }
    y += L.afterStrip;
  }
  const half = (inner - L.columnGap) / 2;
  // list draws a titled column of named counts, each with an optional detail line under it, and returns how tall it drew.
  const list = (title: string, x: number, rows: { name: string; value: string; mono: boolean; detail?: string }[]): number => {
    parts.push(text({ x, y, text: title, size: L.label, bold: true, color: COLORS.muted }));
    let rowY = y + L.listGap;
    for (const r of rows) {
      parts.push(
        text({ x, y: rowY, text: wrap(r.name, L.row, half - L.guardTextInset, 1)[0] ?? "", size: L.row, mono: r.mono }),
        text({ x: x + half, y: rowY, text: r.value, size: L.row, end: true }),
      );
      if (r.detail) {
        const detail = wrap(r.detail, L.detail, half, 1)[0] ?? "";
        parts.push(text({ x, y: rowY + L.detailGap, text: detail, size: L.detail, color: COLORS.faint }));
        rowY += L.detailGap;
      }
      rowY += L.rowGap;
    }
    return rowY - y - L.listGap;
  };
  const programs = card.programs.map((p) => ({ name: p.name, value: p.count.toLocaleString(), mono: true }));
  // Each group names its domains on a line under it when they fit above the footer with what follows; a card too full for them
  // shows the groups alone rather than overlap.
  const guardHeight = card.guard ? L.guardPad + GUARD_LINES * L.guardLine : 0;
  const withDomains = L.afterList + Math.max(card.programs.length * L.rowGap, card.destinations.length * (L.rowGap + L.detailGap));
  const roomy = y + withDomains + guardHeight <= height - L.pad - L.footerRoom;
  const places = card.destinations.map((d) => ({ name: d.label, value: d.count.toLocaleString(), mono: false,
    ...(roomy && d.domains.length > 0 ? { detail: d.domains.join(", ") } : {}) }));
  const drawn = Math.max(programs.length > 0 ? list("Programs it ran most", L.pad, programs) : 0,
    places.length > 0 ? list("Where it talked to", programs.length > 0 ? L.pad + half + L.columnGap : L.pad, places) : 0);
  if (drawn > 0) y += L.afterList + drawn;
  if (card.guard) {
    const lines = wrap(guardLine(card), L.guardText, inner - L.guardTextInset - L.guardInset, GUARD_LINES);
    const box = L.guardPad + lines.length * L.guardLine;
    parts.push(
      rect(L.pad, y, inner, box, COLORS.panel, L.guardRadius),
      shield(L.pad + L.guardInset, y + box / 2 - L.guardIcon / 2, L.guardIcon),
    );
    lines.forEach((line, i) => {
      const lineY = y + L.guardPad + i * L.guardLine + L.guardBaseline;
      parts.push(text({ x: L.pad + L.guardTextInset, y: lineY, text: line, size: L.guardText }));
    });
  }
  parts.push(footer(width, L.pad, height - L.pad, L.footer));
  return frame(width, height, parts);
}

function landscape(card: ShareCard): string {
  const L = LANDSCAPE;
  const { width, height } = SIZES.landscape;
  const column = (width - 2 * L.pad - L.columnGap) / 2;
  const right = L.pad + column + L.columnGap;
  const parts: string[] = [];
  let y = L.top + L.label;
  const heading = wrap([sessionLabel(card), card.date].filter((s) => s !== null).join(" · "), L.label, column / CAPS_WIDTH, 1)[0] ?? "";
  parts.push(text({ x: L.pad, y, text: heading, size: L.label, color: COLORS.muted, bold: true, caps: true }));
  y += L.labelGap;
  for (const line of wrap(card.title ?? headline(card), L.title, column, TITLE_LINES)) {
    y += L.titleLine;
    parts.push(text({ x: L.pad, y: y - L.titleBaseline, text: line, size: L.title, bold: true }));
  }
  y += L.afterTitle;
  stats(card).forEach((s, i) => {
    const x = L.pad + (i % 2) * (column / 2);
    const top = y + Math.floor(i / 2) * L.statRow;
    parts.push(
      text({ x, y: top + L.stat, text: s.value, size: L.stat, bold: true, color: i === 0 ? COLORS.accent : COLORS.text }),
      text({ x, y: top + L.stat + L.statLabelGap, text: s.label, size: L.label, color: COLORS.muted }),
    );
  });
  let ry = L.top + L.label;
  if (card.timeline.length > 0) {
    parts.push(
      text({ x: right, y: ry, text: "When it was busy", size: L.label, bold: true, color: COLORS.muted }),
      bars(card.timeline, right, ry + L.stripGap, column, L.strip),
    );
    ry += L.afterStrip;
  }
  if (card.programs.length > 0) {
    parts.push(text({ x: right, y: ry, text: "Programs it ran most", size: L.label, bold: true, color: COLORS.muted }));
    const names = card.programs.map((p) => `${p.name} ${p.count.toLocaleString()}`).join("   ");
    parts.push(text({ x: right, y: ry + L.rowGap, text: wrap(names, L.row, column, 1)[0] ?? "", size: L.row, mono: true }));
    ry += L.afterRow;
  }
  if (card.destinations.length > 0) {
    parts.push(text({ x: right, y: ry, text: "Where it talked to", size: L.label, bold: true, color: COLORS.muted }));
    const groups = card.destinations.map((d) => (d.domains.length > 0 ? `${d.label}: ${d.domains.join(", ")}` : d.label)).join(" · ");
    const lines = wrap(groups, L.row, column, 2);
    lines.forEach((line, i) => { parts.push(text({ x: right, y: ry + L.rowGap + i * L.rowLine, text: line, size: L.row })); });
    ry += L.rowGap + lines.length * L.rowLine + L.afterGroups - L.rowLine;
  }
  if (card.guard) {
    const lines = wrap(guardLine(card, true), L.label, column - L.guardTextInset - L.guardInset, GUARD_LINES);
    const box = L.guardPad + lines.length * L.guardLine;
    parts.push(
      rect(right, ry, column, box, COLORS.panel, L.guardRadius),
      shield(right + L.guardInset, ry + (box - L.guardIcon) / 2, L.guardIcon),
    );
    lines.forEach((line, i) => {
      parts.push(text({ x: right + L.guardTextInset, y: ry + L.guardPad / 2 + (i + 1) * L.guardLine - L.guardBaseline, text: line,
        size: L.label }));
    });
  }
  parts.push(footer(width, L.pad, height - L.footerBottom, L.footer));
  return frame(width, height, parts);
}

function frame(width: number, height: number, parts: readonly string[]): string {
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${String(width)}" height="${String(height)}" `
    + `viewBox="0 0 ${String(width)} ${String(height)}"><rect width="100%" height="100%" fill="${COLORS.ground}"/>${parts.join("")}</svg>`;
}

// shareSvg draws the card in the chosen size.
export function shareSvg(card: ShareCard, size: ShareSize): string {
  return size === "portrait" ? portrait(card) : landscape(card);
}

// svgDataUrl is the SVG as a URL an <img> shows without a request.
export function svgDataUrl(svg: string): string {
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
}

// sharePng draws the SVG onto a canvas SCALE times its size and returns the PNG, all in the browser.
export async function sharePng(svg: string, size: ShareSize): Promise<Blob> {
  const { width, height } = size === "portrait" ? SIZES.portrait : SIZES.landscape;
  const image = new Image();
  image.src = svgDataUrl(svg);
  await image.decode();
  const canvas = document.createElement("canvas");
  canvas.width = width * SCALE;
  canvas.height = height * SCALE;
  const context = canvas.getContext("2d");
  if (!context) throw new Error("This browser cannot draw the image.");
  context.drawImage(image, 0, 0, canvas.width, canvas.height);
  return new Promise((resolve, reject) => {
    canvas.toBlob((blob) => { if (blob) resolve(blob); else reject(new Error("This browser could not make the image.")); }, "image/png");
  });
}
