import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ReauthModal } from "./ReauthModal";
import * as auth from "../auth";
import { registerReauthDraft, readReauthDraft } from "../reauthDraft";
import type { ReauthChallenge } from "../api";

// ReauthModal renders the per-flow reauth prompt and dispatches to either an OIDC redirect or the WebAuthn break-glass
// ceremony depending on challenge.authMethod. Tests cover:
//   - null challenge → renders nothing (early-return branch)
//   - OIDC flow: renders the right copy + button; click → reauthOIDC; Cancel → resolve(false)
//   - break-glass flow: renders password input + submit; submit happy-path calls reauthBreakglass + resolve(true);
//     each documented BreakglassError reason renders the right operator copy; WebAuthn NotAllowedError renders
//     the right copy; non-mapped error falls through to the generic message.
//
// JSDOM ships HTMLDialogElement but its showModal()/close() are stubs that don't bubble events the same way as a real
// browser; the test only inspects what's rendered, not the dialog's modal-ness.

const oidcChallenge: ReauthChallenge = {
  authMethod: "oidc",
  reauthURL: "/api/auth/login?reauth=1",
};

const breakglassChallenge: ReauthChallenge = {
  authMethod: "local_password",
  reauthURL: "/api/auth/reauth",
};

// Save the originals once so afterEach can put them back. Without this, the prototype mutation below leaks across test
// files in the same vitest worker and changes ReauthModal's render behaviour anywhere else HTMLDialogElement is used
// (Copilot + CodeRabbit + Gemini #278). The eslint-disable-next-line silences @typescript-eslint/unbound-method: we're
// not calling these references directly, just storing them for later prototype reassignment.
// eslint-disable-next-line @typescript-eslint/unbound-method
const originalShowModal = HTMLDialogElement.prototype.showModal;
// eslint-disable-next-line @typescript-eslint/unbound-method
const originalClose = HTMLDialogElement.prototype.close;

beforeEach(() => {
  // showModal/close in jsdom default to throwing; install stubs that match the real behaviour testing-library needs: the
  // `open` attribute MUST be set/unset so getByRole queries treat the dialog's children as accessible. Without that the
  // role-based queries fail with "no accessible roles" because a non-`open` dialog is treated as inert.
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute("open");
  };
});

afterEach(() => {
  vi.restoreAllMocks();
  // Put back the originals so this file's prototype mutation doesn't bleed into other test files running in the same
  // vitest worker. vi.restoreAllMocks() handles vi.spyOn spies but doesn't touch direct prototype assignments.
  HTMLDialogElement.prototype.showModal = originalShowModal;
  HTMLDialogElement.prototype.close = originalClose;
});

describe("ReauthModal", () => {
  it("renders nothing when challenge is null", () => {
    const { container } = render(<ReauthModal open={false} challenge={null} resolve={vi.fn()} />);
    expect(container.querySelector("dialog")).toBeNull();
  });

  it("renders the OIDC flow's copy when authMethod is 'oidc'", () => {
    render(<ReauthModal open={true} challenge={oidcChallenge} resolve={vi.fn()} />);
    expect(screen.getByRole("heading", { name: /confirm your identity/i })).toBeInTheDocument();
    expect(screen.getByText(/identity provider/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /continue with single sign-on/i })).toBeInTheDocument();
  });

  it("OIDC Continue triggers reauthOIDC with the server-supplied reauthURL", () => {
    const reauthSpy = vi.spyOn(auth, "reauthOIDC").mockImplementation(() => undefined);
    render(<ReauthModal open={true} challenge={oidcChallenge} resolve={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: /continue with single sign-on/i }));
    expect(reauthSpy).toHaveBeenCalledWith("/api/auth/login?reauth=1");
  });

  it("OIDC Cancel calls resolve(false) without redirecting", () => {
    const resolve = vi.fn();
    const reauthSpy = vi.spyOn(auth, "reauthOIDC").mockImplementation(() => undefined);
    render(<ReauthModal open={true} challenge={oidcChallenge} resolve={resolve} />);
    fireEvent.click(screen.getByRole("button", { name: /^cancel$/i }));
    expect(resolve).toHaveBeenCalledWith(false);
    expect(reauthSpy).not.toHaveBeenCalled();
  });

  it("renders the break-glass form when authMethod is 'local_password'", () => {
    render(<ReauthModal open={true} challenge={breakglassChallenge} resolve={vi.fn()} />);
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /confirm with security key/i })).toBeInTheDocument();
  });

  it("break-glass submit happy path calls reauthBreakglass + resolve(true)", async () => {
    const reauthSpy = vi.spyOn(auth, "reauthBreakglass").mockResolvedValue(undefined);
    const resolve = vi.fn();
    render(<ReauthModal open={true} challenge={breakglassChallenge} resolve={resolve} />);

    const passwordInput = screen.getByLabelText(/password/i);
    fireEvent.change(passwordInput, { target: { value: "ops-password" } });
    fireEvent.click(screen.getByRole("button", { name: /confirm with security key/i }));

    await waitFor(() => { expect(reauthSpy).toHaveBeenCalledWith("ops-password"); });
    await waitFor(() => { expect(resolve).toHaveBeenCalledWith(true); });
  });

  it("break-glass invalid_credentials renders the directed message", async () => {
    vi.spyOn(auth, "reauthBreakglass").mockRejectedValue(
      new auth.BreakglassError(401, "invalid_credentials"),
    );
    const resolve = vi.fn();
    render(<ReauthModal open={true} challenge={breakglassChallenge} resolve={resolve} />);

    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "wrong" } });
    fireEvent.click(screen.getByRole("button", { name: /confirm with security key/i }));

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(/invalid password or security key/i));
    expect(resolve).not.toHaveBeenCalled();
  });

  it("break-glass rate_limited renders the directed message", async () => {
    vi.spyOn(auth, "reauthBreakglass").mockRejectedValue(
      new auth.BreakglassError(429, "rate_limited"),
    );
    render(<ReauthModal open={true} challenge={breakglassChallenge} resolve={vi.fn()} />);

    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "pw" } });
    fireEvent.click(screen.getByRole("button", { name: /confirm with security key/i }));

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(/too many attempts/i));
  });

  it("break-glass unknown reason falls through to the generic message", async () => {
    vi.spyOn(auth, "reauthBreakglass").mockRejectedValue(
      new auth.BreakglassError(500, "some_unmapped_reason"),
    );
    render(<ReauthModal open={true} challenge={breakglassChallenge} resolve={vi.fn()} />);

    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "pw" } });
    fireEvent.click(screen.getByRole("button", { name: /confirm with security key/i }));

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(/reauth failed/i));
  });

  it("break-glass WebAuthn NotAllowedError renders the security-key cancelled message", async () => {
    const webauthnCancel = new DOMException("user cancelled", "NotAllowedError");
    vi.spyOn(auth, "reauthBreakglass").mockRejectedValue(webauthnCancel);
    render(<ReauthModal open={true} challenge={breakglassChallenge} resolve={vi.fn()} />);

    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "pw" } });
    fireEvent.click(screen.getByRole("button", { name: /confirm with security key/i }));

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(/security-key prompt was cancelled/i));
  });

  it("break-glass Cancel button calls resolve(false)", () => {
    const resolve = vi.fn();
    render(<ReauthModal open={true} challenge={breakglassChallenge} resolve={resolve} />);
    fireEvent.click(screen.getByRole("button", { name: /^cancel$/i }));
    expect(resolve).toHaveBeenCalledWith(false);
  });

  it("break-glass submit is disabled until a password is entered", () => {
    render(<ReauthModal open={true} challenge={breakglassChallenge} resolve={vi.fn()} />);
    const submit = screen.getByRole("button", { name: /confirm with security key/i });
    expect(submit).toBeDisabled();
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "p" } });
    expect(submit).not.toBeDisabled();
  });
});

// An OIDC reauthentication leaves the page, so the dialog keeps the page's unsaved work before it goes and says so (issue #1113). It
// is never replayed: signing in does not save anything.
describe("ReauthModal OIDC flow and unsaved work", () => {
  const off: (() => void)[] = [];
  afterEach(() => {
    while (off.length > 0) off.pop()?.();
    sessionStorage.clear();
  });

  // spec:web-ui/unsaved-work-survives-a-reauthentication/unsaved-work-is-kept-across-the-sign-in
  it("keeps the unsaved work before it leaves the page, and says it will be put back and not saved", () => {
    off.push(registerReauthDraft("surface", () => ({ text: "typed" })));
    const reauthSpy = vi.spyOn(auth, "reauthOIDC").mockImplementation(() => {
      // Persisted before the page goes: by the time the redirect runs, the work is already in storage.
      expect(readReauthDraft("surface")).toEqual({ text: "typed" });
    });
    render(<ReauthModal open={true} challenge={oidcChallenge} resolve={vi.fn()} />);

    expect(screen.getByText(/unsaved changes on this page are kept/i)).toBeVisible();
    expect(screen.getByText(/does not save them or re-run the action/i)).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: /continue with single sign-on/i }));

    expect(reauthSpy).toHaveBeenCalledTimes(1);
  });

  it("says nothing about unsaved work when the page has none, and saves nothing", () => {
    off.push(registerReauthDraft("surface", () => null));
    const reauthSpy = vi.spyOn(auth, "reauthOIDC").mockImplementation(() => undefined);
    render(<ReauthModal open={true} challenge={oidcChallenge} resolve={vi.fn()} />);

    expect(screen.queryByText(/unsaved changes/i)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /continue with single sign-on/i }));

    expect(reauthSpy).toHaveBeenCalledTimes(1);
    expect(readReauthDraft("surface")).toBeUndefined();
  });

  // spec:web-ui/unsaved-work-survives-a-reauthentication/work-that-cannot-be-kept-is-said-to-be-lost
  it("will not leave silently when the work could not be kept: it says so, and a second click goes ahead", () => {
    off.push(registerReauthDraft("surface", () => ({ text: "typed" })));
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("full", "QuotaExceededError");
    });
    const reauthSpy = vi.spyOn(auth, "reauthOIDC").mockImplementation(() => undefined);
    render(<ReauthModal open={true} challenge={oidcChallenge} resolve={vi.fn()} />);

    fireEvent.click(screen.getByRole("button", { name: /continue with single sign-on/i }));

    expect(reauthSpy).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent(/could not be kept, and will be lost/i);
    fireEvent.click(screen.getByRole("button", { name: /continue anyway/i }));
    expect(reauthSpy).toHaveBeenCalledTimes(1);
  });

  it("never touches the work on the break-glass flow, which does not leave the page", () => {
    off.push(registerReauthDraft("surface", () => ({ text: "typed" })));
    render(<ReauthModal open={true} challenge={breakglassChallenge} resolve={vi.fn()} />);

    expect(screen.queryByText(/unsaved changes/i)).toBeNull();
    expect(readReauthDraft("surface")).toBeUndefined();
  });
});
