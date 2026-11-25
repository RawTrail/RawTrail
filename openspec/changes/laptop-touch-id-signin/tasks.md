## 1. Server

- [x] 1.1 `BeginPasskeyLogin` requires user verification; `FinishPasskeyLogin` refuses a challenge without it and checks no password
- [x] 1.2 Mount the passkey-only routes only on a laptop install, and audit the sign-in as `passkey_only`

## 2. Console

- [x] 2.1 One "Sign in with Touch ID" button on a laptop, for the remembered email or `admin@rawtrail.local`
- [x] 2.2 The email, password and passkey form a click away with the email filled in, and on a failed Touch ID sign-in

## 3. Docs

- [x] 3.1 `docs/breakglass.md` and the OpenAPI spec describe the passkey-only sign-in
