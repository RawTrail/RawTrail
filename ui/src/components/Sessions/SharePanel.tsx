import { useState } from "react";
import { Button } from "../ui/Button";
import { caption, type ShareCard } from "./shareCard";
import { sharePng } from "./shareImage";

const FILE_NAME = "rawtrail-session.png";

// composerUrl opens a post composer with the caption filled in. The image is pasted by the person: neither site takes one in a link.
function composerUrl(site: "linkedin" | "x", text: string): string {
  const q = encodeURIComponent(text);
  return site === "linkedin" ? `https://www.linkedin.com/feed/?shareActive=true&text=${q}` : `https://x.com/intent/post?text=${q}`;
}

interface SharePanelProps {
  // card and svg are what the Summary shows, so what is shared is what the person sees; null until the host's name is read.
  readonly card: ShareCard | null;
  readonly svg: string | null;
  // hostUnread says the host's name could not be read, so the card is not drawn and nothing is offered.
  readonly hostUnread: boolean;
}

// SharePanel shares the Summary's card as an image (issue #1430): it copies or downloads the PNG, copies a suggested caption, and opens
// the LinkedIn or X composer. The PNG is made in the browser from the card shown beside it; nothing is uploaded and there are no share
// links.
export function SharePanel({ card, svg, hostUnread }: SharePanelProps) {
  const [status, setStatus] = useState<string | null>(null);
  const words = card ? caption(card) : "";
  const ready = card !== null && svg !== null && !hostUnread;

  async function copyImage() {
    if (!svg) return;
    const clipboard = navigator.clipboard as Clipboard | undefined;
    if (!clipboard || typeof ClipboardItem === "undefined") {
      setStatus("This browser cannot copy an image. Download it instead.");
      return;
    }
    try {
      // The item takes the PNG as a promise, which Safari requires so the copy stays inside the click.
      await clipboard.write([new ClipboardItem({ "image/png": sharePng(svg) })]);
      setStatus("Image copied.");
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

  return (
    <section className="share-panel" aria-label="Share as an image">
      <h2 className="share-panel__title">Share as an image</h2>
      <p className="share-panel__note">
        The image is the card shown here, made in your browser. Nothing is uploaded, and there are no share links. Usernames, hostnames
        and IP addresses are always removed, home folders show as ~, and commands show as program names, never their arguments.
      </p>
      {hostUnread && (
        <p className="share-panel__error" role="alert">
          This host&apos;s name could not be read, so the card cannot be checked for it and is not shown. Try again later.
        </p>
      )}
      <div className="share-panel__buttons">
        <Button disabled={!ready} onClick={() => { void copyImage(); }}>Copy image</Button>
        <Button variant="inverse" disabled={!ready} onClick={() => { void download(); }}>Download PNG</Button>
        <Button variant="inverse" disabled={!ready} onClick={() => { void copyCaption(); }}>Copy caption</Button>
      </div>
      {ready && (
        <>
          <label className="share-panel__field">
            <span>Suggested caption</span>
            <textarea readOnly rows={3} value={words} />
          </label>
          <p className="share-panel__links">
            <a href={composerUrl("linkedin", words)} target="_blank" rel="noopener noreferrer">Open LinkedIn</a>
            <a href={composerUrl("x", words)} target="_blank" rel="noopener noreferrer">Open X</a>
          </p>
        </>
      )}
      <output className="share-panel__status">{status}</output>
    </section>
  );
}
