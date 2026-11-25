import { describe, it, expect } from "vitest";
import { BreakglassError } from "../auth";
import { consoleRoute, signInErrorMessage } from "./signIn";

describe("signInErrorMessage", () => {
  it.each([
    ["invalid_credentials", "Invalid email, password, or security key."],
    ["no_credentials", "No security key is registered for this account."],
    ["email_rate_limited", "Too many failed attempts for this email. Wait a minute and try again."],
  ])("gives the copy for a server reason, %s", (reason, want) => {
    expect(signInErrorMessage(new BreakglassError(400, reason))).toBe(want);
  });

  it("falls back to the generic copy for an unknown reason, another error or a non-error", () => {
    expect(signInErrorMessage(new BreakglassError(500, "http_500"))).toBe("Sign-in failed. Please try again.");
    expect(signInErrorMessage(new Error("network gone"))).toBe("Sign-in failed. Please try again.");
    expect(signInErrorMessage("oops")).toBe("Sign-in failed. Please try again.");
  });

  it("says a refused ceremony was cancelled, as the browser or @simplewebauthn reports it", () => {
    const cancelled = "Security-key sign-in was cancelled or timed out. Please try again.";
    expect(signInErrorMessage(new DOMException("cancelled", "NotAllowedError"))).toBe(cancelled);
    expect(signInErrorMessage(Object.assign(new Error("not allowed"), { name: "NotAllowedError" }))).toBe(cancelled);
  });
});

describe("consoleRoute", () => {
  it.each([
    ["/ui", "/"],
    ["/ui/", "/"],
    ["/ui/alerts", "/alerts"],
    ["/uipreview", "/uipreview"],
    ["/elsewhere", "/elsewhere"],
  ])("routes %s to %s", (redirect, want) => {
    expect(consoleRoute(redirect)).toBe(want);
  });
});
