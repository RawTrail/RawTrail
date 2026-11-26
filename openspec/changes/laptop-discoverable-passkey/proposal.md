## Why

On a laptop install, "Sign in with Touch ID" signs in a known account: the email of the last sign-in in that browser, or `admin@rawtrail.local` (#1617). An owner in a fresh browser, after clearing site data, or with another account got the email and password form once before Touch ID worked again (#1619, raised by pr-agent in review of #1617).

## What Changes

- **Server:** on a laptop install, the passkey-only challenge requested with no email names no account and lists no credentials, so the browser offers any passkey it holds for the site. The finish signs in the account the passkey's user handle names, after go-webauthn checks that the handle owns the credential. A handle that names no break-glass account is refused (`not_breakglass`, or `user_not_found`). The per-email budget is spent on the account the handle names. The password sign-in still names its account by email only.
- **Setup:** a laptop install asks the authenticator for a discoverable credential (`residentKey: preferred`). Preferred, not required: an authenticator that cannot store one still registers, and signs in through its email. A server install asks for none, which would spend a security key's few discoverable slots and make it set a PIN, for a sign-in a server does not offer.
- **Console:** the Touch ID button uses the remembered email when the browser has one, and otherwise asks for any passkey. The fallback form is filled in with the remembered email or `admin@rawtrail.local`.
- **Spec:** ADDED "A laptop's passkey sign-in can name no account" (`server-identity-authentication`). The web-ui requirement for the laptop page lives only in the unarchived `laptop-passkey-login` delta, so its scenario is edited there in place rather than modified from a second change.

## Impact

- `server/identity/internal/breakglass` (`service.go`, `handler.go`, `webauthn.go`) and `server/identity/bootstrap` (the laptop flag reaches the service).
- `ui/src/components/LaptopSignIn.tsx`, `ui/src/auth.ts`.
- `docs/breakglass.md`.
