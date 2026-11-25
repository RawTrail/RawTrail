// A laptop install's sign-in: one "Sign in with Touch ID" button, which signs the owner in with their passkey alone, the authenticator
// verifying them. The server offers that only on a laptop. It signs in the email the owner last signed in with, the laptop's admin
// account by default. The email, password and passkey form stays a click away, with that email filled in, for a different account or
// a passkey the browser cannot find; a failed Touch ID sign-in opens it.

import { useState } from "react";
import { useNavigate } from "react-router";
import { Button } from "./ui/Button";
import { breakglassBeginPasskeyLogin, breakglassFinishPasskeyLogin } from "../auth";
import { PasskeySignInForm } from "./PasskeySignInForm";
import { consoleRoute, signInErrorMessage } from "./signIn";

// LAPTOP_ADMIN_EMAIL is the admin account a laptop install creates.
export const LAPTOP_ADMIN_EMAIL = "admin@rawtrail.local";

// SIGN_IN_EMAIL_KEY is where this browser keeps the email of the last laptop sign-in.
export const SIGN_IN_EMAIL_KEY = "rawtrail.laptopSignInEmail";

// rememberedEmail is the email of the last sign-in in this browser, or the laptop's admin account. Browser storage can be blocked, in a
// private window for one, and then the admin account is used.
function rememberedEmail(): string {
  try {
    return localStorage.getItem(SIGN_IN_EMAIL_KEY) ?? LAPTOP_ADMIN_EMAIL;
  } catch {
    return LAPTOP_ADMIN_EMAIL;
  }
}

function rememberEmail(email: string) {
  try {
    localStorage.setItem(SIGN_IN_EMAIL_KEY, email);
  } catch {
    // Nothing to keep it in; the next sign-in uses the admin account.
  }
}

export function LaptopSignIn() {
  const navigate = useNavigate();
  const [email] = useState(rememberedEmail);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);

  async function signInWithTouchID() {
    setError(null);
    setBusy(true);
    try {
      const assertion = await breakglassBeginPasskeyLogin(email);
      const result = await breakglassFinishPasskeyLogin(email, assertion);
      await navigate(consoleRoute(result.redirect), { replace: true });
    } catch (err) {
      setError(signInErrorMessage(err));
      setShowForm(true);
    } finally {
      setBusy(false);
    }
  }

  if (showForm) {
    return (
      <>
        {error && (
          <div className="login-card__error" role="alert">
            {error}
          </div>
        )}
        <PasskeySignInForm submitLabel="Sign in with your passkey" initialEmail={email} onSignedIn={rememberEmail} />
      </>
    );
  }

  return (
    <>
      <Button type="button" fullWidth isLoading={busy} onClick={() => { void signInWithTouchID(); }}>
        Sign in with Touch ID
      </Button>
      <div className="login-card__footer">
        <button type="button" className="login-card__break-glass login-card__link-button" onClick={() => { setShowForm(true); }}>
          Use your email and password
        </button>
      </div>
    </>
  );
}
