import { describe, it, expect, vi, afterEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { LaptopSignIn, SIGN_IN_EMAIL_KEY } from "./LaptopSignIn";
import * as auth from "../auth";

// LaptopSignIn is a laptop install's sign-in: one Touch ID button for the remembered email, or for any passkey when none is remembered,
// and the email, password and passkey form a click away. The Touch ID ceremony and the password ceremony are mocked at the auth layer.

function renderSignIn() {
  return render(
    <MemoryRouter initialEntries={["/login"]}>
      <Routes>
        <Route path="/login" element={<LaptopSignIn />} />
        <Route path="/" element={<div>HOME</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("LaptopSignIn", () => {
  // spec:web-ui/a-laptop-install-signs-in-with-the-owner-s-passkey/the-laptop-login-page-shows-one-passkey-button
  it("signs in the email of the last sign-in with Touch ID", async () => {
    localStorage.setItem(SIGN_IN_EMAIL_KEY, "owner@example.com");
    const begin = vi.spyOn(auth, "breakglassBeginPasskeyLogin").mockResolvedValue({ id: "asrt" });
    const finish = vi.spyOn(auth, "breakglassFinishPasskeyLogin").mockResolvedValue({ redirect: "/ui/" });
    renderSignIn();
    fireEvent.click(screen.getByRole("button", { name: "Sign in with Touch ID" }));
    expect(await screen.findByText("HOME")).toBeVisible();
    expect(begin).toHaveBeenCalledWith("owner@example.com");
    expect(finish).toHaveBeenCalledWith("owner@example.com", { id: "asrt" });
  });

  // spec:web-ui/a-laptop-install-signs-in-with-the-owner-s-passkey/the-laptop-login-page-shows-one-passkey-button
  it("asks for any passkey, naming no account, when no sign-in is remembered", async () => {
    const begin = vi.spyOn(auth, "breakglassBeginPasskeyLogin").mockResolvedValue({ id: "asrt" });
    const finish = vi.spyOn(auth, "breakglassFinishPasskeyLogin").mockResolvedValue({ redirect: "/ui/" });
    renderSignIn();
    fireEvent.click(screen.getByRole("button", { name: "Sign in with Touch ID" }));
    expect(await screen.findByText("HOME")).toBeVisible();
    expect(begin).toHaveBeenCalledWith(undefined);
    expect(finish).toHaveBeenCalledWith(undefined, { id: "asrt" });
  });

  // spec:web-ui/a-laptop-install-signs-in-with-the-owner-s-passkey/the-email-and-password-form-is-a-click-away
  it("opens the form with the email filled in when a Touch ID sign-in fails, and says why", async () => {
    vi.spyOn(auth, "breakglassBeginPasskeyLogin").mockRejectedValue(new auth.BreakglassError(400, "no_credentials"));
    renderSignIn();
    fireEvent.click(screen.getByRole("button", { name: "Sign in with Touch ID" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("No security key is registered for this account.");
    expect(screen.getByLabelText(/email/i)).toHaveValue("admin@rawtrail.local");
    expect(screen.getByRole("button", { name: "Sign in with your passkey" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "Sign in with Touch ID" })).toBeNull();
  });

  // spec:web-ui/a-laptop-install-signs-in-with-the-owner-s-passkey/the-email-and-password-form-is-a-click-away
  it("offers the form a click away, and a sign-in through it is the email the next Touch ID sign-in uses", async () => {
    const begin = vi.spyOn(auth, "breakglassBeginLogin").mockResolvedValue({ id: "asrt" });
    const finish = vi.spyOn(auth, "breakglassFinishLogin").mockResolvedValue({ redirect: "/ui/" });
    renderSignIn();
    fireEvent.click(screen.getByRole("button", { name: "Use your email and password" }));
    const email = screen.getByLabelText(/email/i);
    expect(email).toHaveValue("admin@rawtrail.local");
    expect(screen.getByLabelText(/password/i)).toHaveFocus();
    fireEvent.change(email, { target: { value: "second@example.com" } });
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "pw" } });
    fireEvent.click(screen.getByRole("button", { name: "Sign in with your passkey" }));
    expect(await screen.findByText("HOME")).toBeVisible();
    expect(begin).toHaveBeenCalledWith("second@example.com");
    expect(finish).toHaveBeenCalledWith("second@example.com", "pw", { id: "asrt" });
    expect(localStorage.getItem(SIGN_IN_EMAIL_KEY)).toBe("second@example.com");
  });

  it("says a Touch ID that was cancelled or not verified was cancelled, as @simplewebauthn reports it", async () => {
    const refused = Object.assign(new Error("The operation either timed out or was not allowed."), { name: "NotAllowedError" });
    vi.spyOn(auth, "breakglassBeginPasskeyLogin").mockRejectedValue(refused);
    renderSignIn();
    fireEvent.click(screen.getByRole("button", { name: "Sign in with Touch ID" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Security-key sign-in was cancelled or timed out. Please try again.");
  });

  it("offers no switch to the form while a Touch ID sign-in is pending", async () => {
    vi.spyOn(auth, "breakglassBeginPasskeyLogin").mockReturnValue(new Promise(() => undefined));
    renderSignIn();
    fireEvent.click(screen.getByRole("button", { name: "Sign in with Touch ID" }));
    expect(await screen.findByRole("button", { name: "Use your email and password" })).toBeDisabled();
  });

  it("asks for any passkey when browser storage is blocked, and fills the form in with the admin account", async () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("blocked", "SecurityError");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("blocked", "SecurityError");
    });
    const begin = vi.spyOn(auth, "breakglassBeginPasskeyLogin").mockRejectedValue(new auth.BreakglassError(401, "invalid_credentials"));
    renderSignIn();
    fireEvent.click(screen.getByRole("button", { name: "Sign in with Touch ID" }));
    expect(await screen.findByRole("alert")).toBeVisible();
    expect(begin).toHaveBeenCalledWith(undefined);
    expect(screen.getByLabelText(/email/i)).toHaveValue("admin@rawtrail.local");
  });
});
