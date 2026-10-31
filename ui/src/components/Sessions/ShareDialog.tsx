import { useEffect, useId, useMemo, useRef, useState } from "react";
import "./ShareDialog.scss";
import { getHostDetail } from "../../api";
import type { AgentSessionTotals, Alert } from "../../types";
import { Button } from "../ui/Button";
import { Toggle } from "../ui/Toggle";
import { altText, caption, DEFAULT_SHARE_OPTIONS, shareCard, TITLE_MAX, type ShareOptions } from "./shareCard";
import { shareSvg, sharePng, SIZES, svgDataUrl, type ShareSize } from "./shareImage";
import { homeUsernames } from "./shareRedact";

interface ShareDialogProps {
  readonly hostId: string;
  readonly totals: AgentSessionTotals;
  readonly alerts: readonly Alert[];
  readonly agent: string;
  readonly project: string | undefined;
  readonly firstNs: number | undefined;
  readonly lastNs: number | undefined;
  readonly onClose: () => void;
}

const FILE_NAME = "rawtrail-session.png";

// HostName is the host's name as the dialog read it: needed before anything is shared, since it is the name redaction must remove.
type HostName = { state: "loading" } | { state: "read"; hostname: string } | { state: "failed" };

// composerUrl opens a post composer with the caption filled in. The image is pasted by the person: neither site takes one in a link.
function composerUrl(site: "linkedin" | "x", text: string): string {
  const q = encodeURIComponent(text);
  return site === "linkedin" ? `https://www.linkedin.com/feed/?shareActive=true&text=${q}` : `https://x.com/intent/post?text=${q}`;
}

// ShareDialog shares a session as an image (issue #1430): a live preview of exactly what is exported, the choices of what to show,
// and the ways out. The image is drawn in the browser from the redacted share card; nothing is uploaded and there are no share links.
// It is rendered only while open; the native <dialog> supplies the backdrop, focus trap, and Escape to close.
export function ShareDialog({ hostId, totals, alerts, agent, project, firstNs, lastNs, onClose }: ShareDialogProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const [host, setHost] = useState<HostName>({ state: "loading" });
  const [size, setSize] = useState<ShareSize>("portrait");
  const [titleOn, setTitleOn] = useState(false);
  const [title, setTitle] = useState("");
  const [options, setOptions] = useState<Omit<ShareOptions, "title">>(DEFAULT_SHARE_OPTIONS);
  const [status, setStatus] = useState<string | null>(null);

  useEffect(() => {
    const dlg = dialogRef.current;
    if (!dlg) return undefined;
    if (!dlg.open) dlg.showModal();
    const onCancel = (e: Event) => { e.preventDefault(); onClose(); };
    const onBackdrop = (e: MouseEvent) => { if (e.target === dlg) onClose(); };
    dlg.addEventListener("cancel", onCancel);
    dlg.addEventListener("click", onBackdrop);
    return () => {
      dlg.removeEventListener("cancel", onCancel);
      dlg.removeEventListener("click", onBackdrop);
    };
  }, [onClose]);

  useEffect(() => {
    let live = true;
    getHostDetail(hostId)
      .then((d) => { if (live) setHost({ state: "read", hostname: d.hostname }); })
      .catch(() => { if (live) setHost({ state: "failed" }); });
    return () => { live = false; };
  }, [hostId]);

  const card = useMemo(() => {
    if (host.state !== "read") return null;
    const directories = (totals.digest?.directories ?? []).map((d) => d.directory);
    const identities = { usernames: homeUsernames(project, ...directories), hostnames: host.hostname ? [host.hostname] : [] };
    return shareCard({ totals, alerts, agent, project, firstNs, lastNs, identities }, { ...options, title: titleOn ? title : null });
  }, [host, totals, alerts, agent, project, firstNs, lastNs, options, titleOn, title]);
  const svg = useMemo(() => (card ? shareSvg(card, size) : null), [card, size]);
  const words = card ? caption(card) : "";

  async function copyImage() {
    if (!svg) return;
    const clipboard = navigator.clipboard as Clipboard | undefined;
    if (!clipboard || typeof ClipboardItem === "undefined") {
      setStatus("This browser cannot copy an image. Download it instead.");
      return;
    }
    try {
      // The item takes the PNG as a promise, which Safari requires so the copy stays inside the click.
      await clipboard.write([new ClipboardItem({ "image/png": sharePng(svg, size) })]);
      setStatus("Image copied.");
    } catch {
      setStatus("The image could not be copied. Download it instead.");
    }
  }

  async function download() {
    if (!svg) return;
    try {
      const url = URL.createObjectURL(await sharePng(svg, size));
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

  const set = (key: keyof Omit<ShareOptions, "title">) => (e: React.ChangeEvent<HTMLInputElement>) => {
    setOptions((o) => ({ ...o, [key]: e.target.checked }));
  };

  return (
    <dialog ref={dialogRef} className="share-dialog" aria-labelledby={titleId}>
      <header className="share-dialog__header">
        <div>
          <h2 id={titleId} className="share-dialog__title">Share this session as an image</h2>
          <p className="share-dialog__note">The image is made in your browser. Nothing is uploaded, and there are no share links.</p>
        </div>
        <Button variant="text-link" onClick={onClose}>Close</Button>
      </header>
      {host.state === "failed" && (
        <p className="share-dialog__error" role="alert">
          This host&apos;s name could not be read, so the image cannot be checked for it. Try again later.
        </p>
      )}
      <div className="share-dialog__body">
        <figure className="share-dialog__preview">
          <figcaption>Preview, exactly as exported</figcaption>
          {svg && card
            ? <img src={svgDataUrl(svg)} alt={altText(card)} className={`share-dialog__image share-dialog__image--${size}`} />
            : <div className={`share-dialog__image share-dialog__image--${size}`} aria-busy={host.state === "loading"} />}
        </figure>
        <div className="share-dialog__controls">
          <fieldset className="share-dialog__group">
            <legend>Size</legend>
            {Object.entries(SIZES).map(([s, shape]) => (
              <Button key={s} variant={size === s ? "primary" : "inverse"} aria-pressed={size === s}
                onClick={() => { setSize(s as ShareSize); }}>
                {shape.label}
              </Button>
            ))}
          </fieldset>
          <div className="share-dialog__group">
            <h3>Always removed</h3>
            <p>Usernames, hostnames and IP addresses. Home folders show as ~. Commands show as program names, never their arguments.</p>
          </div>
          <fieldset className="share-dialog__group">
            <legend>Hidden unless you show them</legend>
            <Toggle id={`${titleId}-title`} label="Show a prompt as the title" checked={titleOn}
              onChange={(e) => { setTitleOn(e.target.checked); }} />
            {titleOn && (
              <label className="share-dialog__field">
                <span>Title: paste a prompt and edit it before you share</span>
                <input type="text" value={title} maxLength={TITLE_MAX * 2} onChange={(e) => { setTitle(e.target.value); }} />
              </label>
            )}
            <Toggle id={`${titleId}-project`} label="Show the project name" checked={options.showProject} onChange={set("showProject")} />
            <Toggle id={`${titleId}-internal`} label="Show internal domains" checked={options.showInternalDomains}
              onChange={set("showInternalDomains")} />
            <Toggle id={`${titleId}-guard`} label="Show what the credential guard stopped" checked={options.showGuard}
              onChange={set("showGuard")} />
          </fieldset>
          <label className="share-dialog__field">
            <span>Suggested caption</span>
            <textarea readOnly rows={3} value={words} />
          </label>
        </div>
      </div>
      <footer className="share-dialog__actions">
        <div className="share-dialog__links">
          {card && (
            <>
              <a href={composerUrl("linkedin", words)} target="_blank" rel="noopener noreferrer">Open LinkedIn</a>
              <a href={composerUrl("x", words)} target="_blank" rel="noopener noreferrer">Open X</a>
            </>
          )}
        </div>
        <p className="share-dialog__status" role="status">{status}</p>
        <div className="share-dialog__buttons">
          <Button variant="inverse" disabled={!card} onClick={() => { void copyCaption(); }}>Copy caption</Button>
          <Button variant="inverse" disabled={!svg} onClick={() => { void download(); }}>Download PNG</Button>
          <Button disabled={!svg} onClick={() => { void copyImage(); }}>Copy image</Button>
        </div>
      </footer>
    </dialog>
  );
}
