import { useEffect, useState } from "react";
import { Button } from "../ui/Button";
import { caption, type ShareCard } from "./shareCard";
import { sharePng } from "./shareImage";

const FILE_NAME = "rawtrail-session.png";

// SITES are the composers the panel opens. Neither takes an image in a link, so opening one also copies the image to paste in. The
// marks are the sites' own logos (Simple Icons, CC0), drawn in their brand colors as their guidelines ask of a share button.
const SITES: readonly { name: string; color: string; path: string; url: (q: string) => string }[] = [
  {
    name: "LinkedIn",
    color: "#0a66c2",
    path: "M20.447 20.452h-3.554v-5.569c0-1.328-.027-3.037-1.852-3.037-1.853 0-2.136 1.445-2.136 2.939v5.667H9.351V9h3.414v1.561h.046"
      + "c.477-.9 1.637-1.85 3.37-1.85 3.601 0 4.267 2.37 4.267 5.455v6.286zM5.337 7.433a2.062 2.062 0 0 1-2.063-2.065 2.064 2.064 0 1 "
      + "1 2.063 2.065zm1.782 13.019H3.555V9h3.564v11.452zM22.225 0H1.771C.792 0 0 .774 0 1.729v20.542C0 23.227.792 24 1.771 24h20.451"
      + "C23.2 24 24 23.227 24 22.271V1.729C24 .774 23.2 0 22.222 0h.003z",
    url: (q) => `https://www.linkedin.com/feed/?shareActive=true&text=${q}`,
  },
  {
    name: "X",
    color: "#000000",
    path: "M18.901 1.153h3.68l-8.04 9.19L24 22.846h-7.406l-5.8-7.584-6.638 7.584H.474l8.6-9.83L0 1.154h7.594l5.243 6.932ZM17.61 "
      + "20.644h2.039L6.486 3.24H4.298Z",
    url: (q) => `https://x.com/intent/post?text=${q}`,
  },
];

interface SharePanelProps {
  // card and svg are what the Summary shows, so what is shared is what the person sees; null until the host's name is read.
  readonly card: ShareCard | null;
  readonly svg: string | null;
  // hostUnread says the host's name could not be read, so the card is not drawn and nothing is offered.
  readonly hostUnread: boolean;
}

// SharePanel shares the Summary's card as an image (issue #1430): it copies or downloads the PNG, copies a suggested caption, shares
// it to LinkedIn or X by copying the image and opening the site's composer with the caption, and, where the browser has a share sheet
// that takes files, shares the PNG through it. The PNG is made in the browser from the card shown beside it; nothing is uploaded and
// there are no share links.
export function SharePanel({ card, svg, hostUnread }: SharePanelProps) {
  const [status, setStatus] = useState<string | null>(null);
  // sheet is the PNG made ahead for the system share sheet, which must be called while the click still counts as the person's. It is
  // kept with the SVG it was made from, so a card redrawn since is never shared as the old one.
  const [sheet, setSheet] = useState<{ svg: string; file: File } | null>(null);
  const words = card ? caption(card) : "";
  const ready = card !== null && svg !== null && !hostUnread;

  const sheetFile = ready && sheet?.svg === svg ? sheet.file : null;

  useEffect(() => {
    if (!ready || typeof navigator.canShare !== "function") return;
    let live = true;
    void sharePng(svg).then((png) => {
      const file = new File([png], FILE_NAME, { type: "image/png" });
      if (live && navigator.canShare({ files: [file] })) setSheet({ svg, file });
    }, () => undefined);
    return () => { live = false; };
  }, [ready, svg]);

  // copyImage puts the PNG on the clipboard and says so, or says why it could not; then is what to add once it is copied.
  async function copyImage(then = "") {
    if (!svg) return;
    const clipboard = navigator.clipboard as Clipboard | undefined;
    if (!clipboard || typeof ClipboardItem === "undefined") {
      setStatus("This browser cannot copy an image. Download it instead.");
      return;
    }
    try {
      // The item takes the PNG as a promise, which Safari requires so the copy stays inside the click.
      await clipboard.write([new ClipboardItem({ "image/png": sharePng(svg) })]);
      setStatus(`Image copied.${then}`);
    } catch {
      setStatus("The image could not be copied. Download it instead.");
    }
  }

  async function download() {
    if (!svg) return;
    try {
      const url = URL.createObjectURL(await sharePng(svg));
      const a = document.createElement("a");
      a.href = url;
      a.download = FILE_NAME;
      a.click();
      URL.revokeObjectURL(url);
      setStatus("Image downloaded.");
    } catch {
      setStatus("The image could not be made in this browser.");
    }
  }

  async function copyCaption() {
    const clipboard = navigator.clipboard as Clipboard | undefined;
    try {
      if (!clipboard) throw new Error("no clipboard");
      await clipboard.writeText(words);
      setStatus("Caption copied.");
    } catch {
      setStatus("The caption could not be copied; select it and copy it yourself.");
    }
  }

  async function shareSheet() {
    if (!sheetFile) return;
    try {
      await navigator.share({ files: [sheetFile], text: words });
    } catch (err) {
      // Closing the sheet is the person's choice, not a failure.
      if (!(err instanceof DOMException && err.name === "AbortError")) setStatus("The image could not be shared. Copy it instead.");
    }
  }

  return (
    <section className="share-panel" aria-label="Share as an image">
      <h2 className="share-panel__title">Share as an image</h2>
      {hostUnread && (
        <p className="share-panel__error" role="alert">
          This host&apos;s name could not be read, so the card cannot be checked for it and is not shown. Try again later.
        </p>
      )}
      {ready && (
        <div className="share-panel__sites">
          {SITES.map((site) => (
            <a
              key={site.name}
              className="share-panel__site"
              href={site.url(encodeURIComponent(words))}
              target="_blank"
              rel="noopener noreferrer"
              onClick={() => { void copyImage(" Paste it into your post."); }}
            >
              <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true" focusable="false">
                <path d={site.path} fill={site.color} />
              </svg>
              Share on {site.name}
            </a>
          ))}
          {sheetFile && (
            <button type="button" className="share-panel__site" onClick={() => { void shareSheet(); }}>
              <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true" focusable="false">
                <path d="M12 3v12M7 8l5-5 5 5M5 13v6a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-6" fill="none" stroke="currentColor"
                  strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
              More
            </button>
          )}
        </div>
      )}
      <div className="share-panel__buttons">
        <Button variant="inverse" disabled={!ready} onClick={() => { void copyImage(); }}>Copy image</Button>
        <Button variant="inverse" disabled={!ready} onClick={() => { void download(); }}>Download PNG</Button>
        <Button variant="inverse" disabled={!ready} onClick={() => { void copyCaption(); }}>Copy caption</Button>
      </div>
      {ready && (
        <label className="share-panel__field">
          <span>Suggested caption</span>
          <textarea readOnly rows={3} value={words} />
        </label>
      )}
      <output className="share-panel__status">{status}</output>
    </section>
  );
}
