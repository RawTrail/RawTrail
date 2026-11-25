// Break-glass login page. Operator arrives via the "Break-glass login" footer link on /ui/login (or by typing /admin/break-glass
// directly, which the server 302s to /ui/admin/break-glass). The form and its ceremony live in PasskeySignInForm.

import { Link } from "react-router";
import { Card } from "./ui/Card";
import { PasskeySignInForm } from "./PasskeySignInForm";
import "./Login.scss";

export function BreakGlassLogin() {
  return (
    <div className="login-page">
      <Card padding="large" className="login-card">
        <div className="login-card__header">
          <span className="login-card__logo" aria-hidden="true">F</span>
          <h1 className="login-card__title">
            Break-glass <span className="login-card__accent">sign-in</span>
          </h1>
          <p className="login-card__subtitle">
            Recovery sign-in with email + password + security key.
          </p>
        </div>

        <PasskeySignInForm submitLabel="Sign in with security key" />

        <div className="login-card__footer">
          <Link to="/login" className="login-card__break-glass">
            Back to single sign-on
          </Link>
        </div>
      </Card>
    </div>
  );
}
