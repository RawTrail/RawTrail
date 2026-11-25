// The passkey sign-in form: email + password + WebAuthn assertion, the break-glass ceremony. The break-glass page uses it for recovery,
// and a laptop install's login page uses it as its only way in, since a laptop has no single sign-on.
//
// Flow:
//   1. The operator types email + password.
//   2. Submit → POST /admin/break-glass/challenge {email}: the server returns assertion options and sets the signed challenge cookie.
//   3. startAuthentication() prompts the authenticator.
//   4. POST /admin/break-glass with email + password + assertion.
//   5. On success, navigate to the redirect the server returns.
//
// The server collapses every failure mode (unknown email, wrong password, no credentials, sign_count regression) onto an
// X-Edr-Auth-Reason of "invalid_credentials" so the wire response can't be used to enumerate accounts. The audit trail records the
// precise reason.

import { useState } from "react";
import { useNavigate } from "react-router";
import { Button } from "./ui/Button";
import { Input } from "./ui/Input";
import { breakglassBeginLogin, breakglassFinishLogin } from "../auth";
import { consoleRoute, signInErrorMessage } from "./signIn";

interface PasskeySignInFormProps {
  // submitLabel names the button for what it does where the form is shown.
  readonly submitLabel: string;
  // initialEmail prefills the email, as a laptop's sign-in page does with its owner's.
  readonly initialEmail?: string;
  // onSignedIn hears the email of a completed sign-in, before the console opens.
  readonly onSignedIn?: (email: string) => void;
}

export function PasskeySignInForm({ submitLabel, initialEmail = "", onSignedIn }: PasskeySignInFormProps) {
  const navigate = useNavigate();
  const [email, setEmail] = useState(initialEmail);
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submitDisabled = !email.trim() || !password || busy;

  async function handleSubmit(e: React.SyntheticEvent) {
    e.preventDefault();
    if (submitDisabled) return;
    setError(null);
    setBusy(true);
    try {
      const assertion = await breakglassBeginLogin(email.trim());
      const result = await breakglassFinishLogin(email.trim(), password, assertion);
      onSignedIn?.(email.trim());
      await navigate(consoleRoute(result.redirect), { replace: true });
    } catch (err) {
      setError(signInErrorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      {error && (
        <div className="login-card__error" role="alert">
          {error}
        </div>
      )}

      <form onSubmit={(e) => { void handleSubmit(e); }} className="login-card__form">
        <Input
          id="bg-email"
          label="Email"
          type="email"
          autoComplete="username"
          value={email}
          onChange={(e) => { setEmail(e.target.value); }}
          autoFocus={initialEmail === ""}
          disabled={busy}
        />
        <Input
          id="bg-password"
          label="Password"
          type="password"
          autoComplete="current-password"
          value={password}
          onChange={(e) => { setPassword(e.target.value); }}
          autoFocus={initialEmail !== ""}
          disabled={busy}
        />
        <Button type="submit" fullWidth disabled={submitDisabled} isLoading={busy}>
          {busy ? "Signing in…" : submitLabel}
        </Button>
      </form>
    </>
  );
}
