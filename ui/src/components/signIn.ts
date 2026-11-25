// Shared by the sign-in forms: the copy for a failed ceremony, and where the console opens after one succeeds.

import { BreakglassError } from "../auth";

// Reason -> message indirection. Stored as a Map (rather than a plain Record<string, string>) so the read site is `.get(reason)`
// instead of `loginErrorLabels[reason]`: eslint-plugin-security flags computed-property reads on plain objects as object-injection
// sinks when the key is server-derived. Same shape Login.tsx uses for the OIDC error reasons.
const loginErrorLabels = new Map<string, string>([
  ["invalid_credentials", "Invalid email, password, or security key."],
  ["no_credentials", "No security key is registered for this account."],
  ["challenge_missing", "Your sign-in session expired. Please try again."],
  ["challenge_invalid", "Your sign-in session is invalid. Please try again."],
  ["body_invalid", "We couldn't read your sign-in data. Please try again."],
  ["assertion_parse_failed", "We couldn't read your security-key response. Please try again."],
  ["rate_limited", "Too many attempts from this address. Wait a minute and try again."],
  ["email_rate_limited", "Too many failed attempts for this email. Wait a minute and try again."],
]);

// signInErrorMessage is the operator-facing copy for a failed sign-in ceremony.
export function signInErrorMessage(err: unknown): string {
  if (err instanceof BreakglassError) {
    return loginErrorLabels.get(err.reason) ?? "Sign-in failed. Please try again.";
  }
  // The browser refuses a cancelled, timed-out or unverified ceremony as NotAllowedError, which @simplewebauthn wraps in its own
  // WebAuthnError under the same name, so match the name rather than DOMException.
  if ((err instanceof Error || err instanceof DOMException) && err.name === "NotAllowedError") {
    return "Security-key sign-in was cancelled or timed out. Please try again.";
  }
  return "Sign-in failed. Please try again.";
}

// consoleRoute is the router path for the redirect the server returns. Strip the basename only when it is exactly /ui or a
// /ui/-prefixed path: a bare startsWith("/ui") would mis-match unrelated paths like /uipreview, slicing off "/ui" and routing somewhere
// unintended.
export function consoleRoute(redirect: string): string {
  return redirect === "/ui" || redirect.startsWith("/ui/") ? redirect.slice("/ui".length) || "/" : redirect;
}
