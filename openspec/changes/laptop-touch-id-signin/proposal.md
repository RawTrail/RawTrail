## Why

A laptop install's owner signed in with their email, password and Touch ID every time (#1613). On a laptop, break-glass is the owner's only sign-in, the console is reached from the same machine, and a passkey that verifies the person (Touch ID) is already two factors: the device and its owner.

## What Changes

- **Server:** on a laptop install, a passkey-only sign-in at `POST /admin/break-glass/passkey`, with its challenge at `POST /admin/break-glass/passkey/challenge`.
  - The challenge requires user verification, and the sign-in refuses any challenge that did not.
  - No password is checked.
  - The rate limits are those of the password sign-in, and the audit row is `auth.breakglass.success` with `method: passkey_only`.
  - A server install does not mount the routes, so it answers them as any unknown path.
- **Console:** the laptop login page offers one "Sign in with Touch ID" button.
  - It uses the email of the last sign-in in that browser, `admin@rawtrail.local` by default.
  - The email, password and passkey form is a click away with that email filled in, and opens on a failed Touch ID sign-in.
  - This revises the laptop page `laptop-passkey-login` added, in that change's delta, which is not yet archived.
- **Spec:** "Break-glass login lives at a separate path" now allows the laptop exception. It also drops a clause saying `/login` never links the break-glass page, which the server page has always done.
- The reauth for sensitive writes still asks for the password and the passkey.

## Impact

- `server/identity/internal/breakglass`: service, handler and the `PasskeyOnly` option; `server/identity/bootstrap` sets it on a laptop.
- `ui/src/components/LaptopSignIn.tsx`, `Login.tsx`, `PasskeySignInForm.tsx`, `signIn.ts` and `auth.ts`.
- `docs/breakglass.md` and the OpenAPI spec.
