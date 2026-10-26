## 1. Server

- [x] 1.1 `Store.Rotate` sets `expires_at` and refuses a revoked or expired account in the same statement.
- [x] 1.2 The rotate handler accepts an optional `expires_in_days`, returns the account view with the secret, and answers `409` for an account that cannot be rotated.

## 2. Console

- [x] 2.1 Expires column with a badge within 14 days, the expiry on the secret card, and Rotate only on active accounts.

## 3. Docs

- [x] 3.1 `docs/service-accounts.md` describes the rotate lifetime and the refusal.
