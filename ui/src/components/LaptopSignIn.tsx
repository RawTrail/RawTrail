// A laptop install's sign-in: one "Sign in with Touch ID" button, which signs the owner in with their passkey alone, the authenticator
// verifying them. The server offers that only on a laptop. It signs in the email the owner last signed in with in this browser; with
// none, the browser offers any passkey it holds for this site, and the server signs in that passkey's account. The email, password and
// passkey form stays a click away, filled in with that email or the laptop's admin account, for a different account or a passkey the
// browser cannot find; a failed Touch ID sign-in opens it.

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

// rememberedEmail is the email of the last sign-in in this browser, if any. Browser storage can be blocked, in a private window for
// one, and then there is none.
function rememberedEmail(): string | undefined {
  try {
    return localStorage.getItem(SIGN_IN_EMAIL_KEY) ?? undefined;
  } catch {
    return undefined;
  }
}

function rememberEmail(email: string) {
  try {
    localStorage.setItem(SIGN_IN_EMAIL_KEY, email);
  } catch {
    // Nothing to keep it in; the next Touch ID sign-in asks for any passkey instead.
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
        <PasskeySignInForm
          submitLabel="Sign in with your passkey"
          initialEmail={email ?? LAPTOP_ADMIN_EMAIL}
          onSignedIn={rememberEmail}
        />
      </>
    );
  }

  return (
    <>
      <Button type="button" fullWidth isLoading={busy} onClick={() => { void signInWithTouchID(); }}>
        Sign in with Touch ID
      </Button>
      <div className="login-card__footer">
        {/* Disabled while Touch ID is pending: the form's ceremony would replace the pending one's challenge cookie. */}
        <button
          type="button"
          className="login-card__break-glass login-card__link-button"
          disabled={busy}
          onClick={() => { setShowForm(true); }}
        >
          Use your email and password
        </button>
      </div>
    </>
  );
}
