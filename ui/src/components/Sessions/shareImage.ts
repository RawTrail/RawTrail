// The image a session is shared as (issue #1430), which its Summary shows too: an SVG drawn from a share card alone, and the PNG made
// from it in the browser. Nothing here reads anything but the card, so nothing the card leaves out can reach the image, and no request
// is made: the text uses the system's fonts.

import { domainCount, filesNotes, HEADLINE, list, type ShareCard } from "./shareCard";

// SIZE is the card's shape: 4:5 portrait, which a feed shows largest.
export const SIZE = { width: 1080, height: 1350 } as const;

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
const GUARD_LINES = 2;
// L is the card's measurements, in card pixels. Text sizes are what a phone-width feed still reads: 22 px and up on a 1080-wide card.
const L = {
  pad: 80, label: 26, labelGap: 52, dateWidth: 220, title: 64, titleLine: 72, titleBaseline: 14, effort: 30, effortGap: 44,
  afterTitle: 48, stat: 92, statLabelGap: 44, statNoteGap: 36, statNote: 24, statNoteLine: 32, statRow: 224, afterStats: 44,
  row: 30, rowGap: 46, listGap: 50, columnGap: 48, afterList: 56,
  detail: 22, detailGap: 30, footerRoom: 60, guardText: 30, guardLine: 40, guardPad: 48, guardIcon: 44, guardInset: 28,
  guardTextInset: 96, guardRadius: 16, guardBaseline: 8, footer: 24,
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

function shield(x: number, y: number, size: number): string {
  return `<g transform="translate(${String(x)} ${String(y)}) scale(${String(size / ICON_BOX)})" fill="none" stroke="${COLORS.accent}" `
    + `stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3l8 3v6c0 4.5-3.4 8.3-8 9-4.6-.7-8-4.5-8-9V6l8-3z"/>`
    + `<path d="M8.5 12l2.5 2.5 4.5-5"/></g>`;
}

// guardLine says what the credential guard stopped.
function guardLine(card: ShareCard): string {
  if (!card.guard) return "";
  return `The credential guard stopped it reading ${list(card.guard.credentials)}`;
}

// effort is the line under the title: how long the session ran and the tokens it used, those the card has.
function effort(card: ShareCard): string {
  return [card.duration && `Ran for ${card.duration}`, card.tokens && `${card.tokens} tokens`].filter((s): s is string => Boolean(s))
    .join(" · ");
}

// STATS are the card's headline numbers, in two rows of two: what the agent's log reports beside what actually ran, then what it
// changed and where it reached. The processes, the number the card is about, are drawn in the accent.
function stats(card: ShareCard): { value: string; label: string; notes: string[]; accent?: boolean }[] {
  return [
    { value: card.toolCalls.toLocaleString(), label: card.toolCalls === 1 ? "tool call" : "tool calls",
      notes: ["in the agent's own log"] },
    { value: card.processes.toLocaleString(), label: card.processes === 1 ? "process" : "processes", notes: ["actually ran"],
      accent: true },
    { value: card.filesChanged.toLocaleString(), label: card.filesChanged === 1 ? "file changed" : "files changed",
      notes: filesNotes(card) },
    { value: domainCount(card), label: card.domains === 1 && !card.domainsCut ? "domain contacted" : "domains contacted", notes: [] },
  ];
}

// footer credits the recording along the card's bottom edge: a mark, "Recorded with RawTrail", and the project's address.
function footer(width: number, pad: number, y: number, size: number): string {
  const mark = size + MARK.grow;
  return rect(pad, y - size + 2, mark, mark, COLORS.accent, MARK.radius)
    + text({ x: pad + mark + MARK.gap, y, text: "Recorded with RawTrail", size, bold: true })
    + text({ x: width - pad, y, text: PROJECT_URL, size, color: COLORS.faint, end: true });
}

function portrait(card: ShareCard): string {
  const { width, height } = SIZE;
  const inner = width - 2 * L.pad;
  const half = (inner - L.columnGap) / 2;
  const parts: string[] = [];
  let y = L.pad + L.label;
  const heading = wrap(`${card.agent} session`, L.label, (inner - L.dateWidth) / CAPS_WIDTH, 1)[0] ?? "";
  parts.push(text({ x: L.pad, y, text: heading, size: L.label, color: COLORS.muted, bold: true, caps: true }));
  if (card.date) parts.push(text({ x: width - L.pad, y, text: card.date, size: L.label, color: COLORS.muted, end: true }));
  y += L.labelGap + L.titleLine;
  parts.push(text({ x: L.pad, y: y - L.titleBaseline, text: HEADLINE, size: L.title, bold: true }));
  const ran = effort(card);
  if (ran) {
    y += L.effortGap;
    parts.push(text({ x: L.pad, y, text: ran, size: L.effort, color: COLORS.muted }));
  }
  y += L.afterTitle;
  stats(card).forEach((s, i) => {
    const x = L.pad + (i % 2) * (half + L.columnGap);
    const top = y + Math.floor(i / 2) * L.statRow;
    parts.push(
      text({ x, y: top + L.stat, text: s.value, size: L.stat, bold: true, color: s.accent ? COLORS.accent : COLORS.text }),
      text({ x, y: top + L.stat + L.statLabelGap, text: s.label, size: L.label, bold: true, color: COLORS.muted }),
    );
    s.notes.forEach((note, line) => {
      const noteY = top + L.stat + L.statLabelGap + L.statNoteGap + line * L.statNoteLine;
      parts.push(text({ x, y: noteY, text: wrap(note, L.statNote, half, 1)[0] ?? "", size: L.statNote, color: COLORS.faint }));
    });
  });
  y += 2 * L.statRow + L.afterStats;
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

function frame(width: number, height: number, parts: readonly string[]): string {
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${String(width)}" height="${String(height)}" `
    + `viewBox="0 0 ${String(width)} ${String(height)}"><rect width="100%" height="100%" fill="${COLORS.ground}"/>${parts.join("")}</svg>`;
}

// shareSvg draws the card.
export function shareSvg(card: ShareCard): string {
  return portrait(card);
}

// svgDataUrl is the SVG as a URL an <img> shows without a request.
export function svgDataUrl(svg: string): string {
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
}

// sharePng draws the SVG onto a canvas SCALE times its size and returns the PNG, all in the browser.
export async function sharePng(svg: string): Promise<Blob> {
  const { width, height } = SIZE;
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
