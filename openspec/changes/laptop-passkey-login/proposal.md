## Why

A laptop install (`EDR_PROFILE=laptop`) has no single sign-on, yet its sign-in page leads with a full-width "Continue with single sign-on" button that cannot work there, and the way its owner actually signs in, with their passkey, is a small "Break-glass login" link below it (#1587). The page cannot tell it is on a laptop, because the profile is reported only by the session probe, after sign-in.

## What Changes

- The server answers `GET /api/auth/options` without a session, with the deployment profile.
- On a laptop install, the sign-in page shows only the passkey form (email, password, and one "Sign in with your passkey" button), and no single sign-on button or break-glass link.
- On a server install, the page is unchanged. The page shows no sign-in control until it has read the profile, and shows the server page when it cannot read it.

## Impact

- `server/identity/internal/login`: the new public route.
- `ui/src/components/Login.tsx`, and the passkey form moved out of `BreakGlassLogin.tsx` into `PasskeySignInForm.tsx` so both pages share it.
- `docs/api/openapi.yaml`: the new route.
