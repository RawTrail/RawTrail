import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { SharePanel } from "./SharePanel";
import type { ShareCard } from "./shareCard";
import { sharePng } from "./shareImage";

vi.mock("./shareImage", () => ({ sharePng: vi.fn(() => Promise.resolve(new Blob(["png"], { type: "image/png" }))) }));

const CARD: ShareCard = {
  agent: "claude-code 2.1.0", date: "Nov 1, 2026", duration: "42 min", tokens: "50k", toolCalls: 12, processes: 140, filesChanged: 3,
  outsideProject: 1, temporary: 0, unrecorded: 0, domains: 2, domainsCut: false, programs: [], destinations: [], guard: null,
};

// stubClipboard gives the page a clipboard, or none, and says whether the browser can put an image on it; sheet adds a system share
// sheet's canShare and share.
function stubClipboard(clipboard: Partial<Clipboard> | undefined, images: boolean, sheet: Partial<Navigator> = {}) {
  vi.stubGlobal("navigator", Object.defineProperties(Object.create(navigator) as Navigator, {
    clipboard: { value: clipboard },
    canShare: { value: sheet.canShare },
    share: { value: sheet.share },
  }));
  vi.stubGlobal("ClipboardItem", images ? class { constructor(readonly items: Record<string, Promise<Blob>>) {} } : undefined);
}

async function press(name: string, says: string) {
  fireEvent.click(screen.getByRole("button", { name }));
  await waitFor(() => { expect(screen.getByText(says)).toBeVisible(); });
}

beforeEach(() => {
  vi.mocked(sharePng).mockClear();
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

// spec:web-ui/a-session-can-be-shared-as-an-image-made-in-the-browser/the-image-is-copied-or-downloaded-and-the-caption-copied
describe("SharePanel", () => {
  it("says to download the image when the browser cannot copy one", async () => {
    stubClipboard({ writeText: vi.fn(() => Promise.resolve()) }, false);
    render(<SharePanel card={CARD} svg="<svg/>" hostUnread={false} />);
    await press("Copy image", "This browser cannot copy an image. Download it instead.");
    expect(sharePng).not.toHaveBeenCalled();
  });

  it("says when the image could not be copied", async () => {
    stubClipboard({ write: vi.fn(() => Promise.reject(new Error("denied"))) }, true);
    render(<SharePanel card={CARD} svg="<svg/>" hostUnread={false} />);
    await press("Copy image", "The image could not be copied. Download it instead.");
  });

  it("says when the image could not be made to download", async () => {
    stubClipboard(undefined, false);
    vi.mocked(sharePng).mockRejectedValueOnce(new Error("no canvas"));
    render(<SharePanel card={CARD} svg="<svg/>" hostUnread={false} />);
    await press("Download PNG", "The image could not be made in this browser.");
  });

  it("asks the person to copy the caption themselves when there is no clipboard", async () => {
    stubClipboard(undefined, false);
    render(<SharePanel card={CARD} svg="<svg/>" hostUnread={false} />);
    await press("Copy caption", "The caption could not be copied; select it and copy it yourself.");
    expect(screen.getByRole("textbox", { name: "Suggested caption" })).toHaveValue(
      "My AI agent's log shows 12 tool calls. The kernel saw 140 processes run. Recorded with RawTrail.");
  });

  it("offers nothing until the card is drawn, nor when the host's name could not be read", () => {
    const { rerender } = render(<SharePanel card={null} svg={null} hostUnread={false} />);
    for (const name of ["Copy image", "Download PNG", "Copy caption"]) expect(screen.getByRole("button", { name })).toBeDisabled();
    expect(screen.queryByRole("link", { name: "Share on X" })).toBeNull();
    rerender(<SharePanel card={CARD} svg="<svg/>" hostUnread />);
    expect(screen.getByRole("alert")).toHaveTextContent("This host's name could not be read");
    expect(screen.getByRole("button", { name: "Copy image" })).toBeDisabled();
  });

  // spec:web-ui/a-session-can-be-shared-as-an-image-made-in-the-browser/a-site-s-composer-opens-with-the-image-copied-to-paste
  it.each([
    ["LinkedIn", /^https:\/\/www\.linkedin\.com\/feed\/\?shareActive=true&text=My%20AI/],
    ["X", /^https:\/\/x\.com\/intent\/post\?text=My%20AI/],
  ])("shares on %s by opening its composer with the caption and copying the image to paste", async (site, href) => {
    const write = vi.fn(() => Promise.resolve());
    stubClipboard({ write }, true);
    render(<SharePanel card={CARD} svg="<svg/>" hostUnread={false} />);
    const link = screen.getByRole("link", { name: `Share on ${site}` });
    expect(link.getAttribute("href")).toMatch(href);
    expect(link).toHaveAttribute("target", "_blank");
    fireEvent.click(link);
    await waitFor(() => { expect(screen.getByText("Image copied. Paste it into your post.")).toBeVisible(); });
    expect(write).toHaveBeenCalledTimes(1);
  });

  // spec:web-ui/a-session-can-be-shared-as-an-image-made-in-the-browser/the-system-share-sheet-takes-the-image-where-the-browser-has-one
  describe("the system share sheet", () => {
    it("shares the PNG and the caption through it when the browser can share the file", async () => {
      const share = vi.fn(() => Promise.resolve());
      stubClipboard(undefined, false, { canShare: () => true, share });
      render(<SharePanel card={CARD} svg="<svg/>" hostUnread={false} />);
      fireEvent.click(await screen.findByRole("button", { name: "More" }));
      await waitFor(() => { expect(share).toHaveBeenCalledTimes(1); });
      const shared = (share.mock.calls[0] as unknown as [ShareData])[0];
      expect(shared.text).toBe("My AI agent's log shows 12 tool calls. The kernel saw 140 processes run. Recorded with RawTrail.");
      expect(shared.files?.map((f) => [f.name, f.type])).toEqual([["rawtrail-session.png", "image/png"]]);
    });

    it("says nothing when the person closes it, and says so when it fails", async () => {
      const share = vi.fn()
        .mockRejectedValueOnce(new DOMException("closed", "AbortError"))
        .mockRejectedValueOnce(new Error("denied"));
      stubClipboard(undefined, false, { canShare: () => true, share });
      render(<SharePanel card={CARD} svg="<svg/>" hostUnread={false} />);
      const more = await screen.findByRole("button", { name: "More" });
      fireEvent.click(more);
      await waitFor(() => { expect(share).toHaveBeenCalledTimes(1); });
      expect(screen.queryByText("The image could not be shared. Copy it instead.")).toBeNull();
      await press("More", "The image could not be shared. Copy it instead.");
    });

    it("is not offered when the browser cannot share a file, nor when it has no share sheet", async () => {
      stubClipboard(undefined, false, { canShare: () => false, share: vi.fn() });
      const { unmount } = render(<SharePanel card={CARD} svg="<svg/>" hostUnread={false} />);
      await waitFor(() => { expect(sharePng).toHaveBeenCalledTimes(1); });
      expect(screen.queryByRole("button", { name: "More" })).toBeNull();
      unmount();
      vi.mocked(sharePng).mockClear();
      stubClipboard(undefined, false);
      render(<SharePanel card={CARD} svg="<svg/>" hostUnread={false} />);
      expect(screen.queryByRole("button", { name: "More" })).toBeNull();
      expect(sharePng).not.toHaveBeenCalled();
    });
  });
});
