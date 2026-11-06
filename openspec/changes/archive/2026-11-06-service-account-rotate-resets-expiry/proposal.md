## Why

Rotating a service account kept its old deadline (#1019). Rotating an account with 3 days left issued a secret that died in 3 days, silently, and the server rotated revoked and expired accounts too, issuing a secret the token endpoint rejects forever with the same opaque error as a wrong secret. The console never showed an account's expiry either: not in the list, and not on the card an operator stores the new secret from.

## What changes

- Rotate sets a fresh expiry from now, 90 days by default, or `expires_in_days` (capped at 365, as on create), and returns the account with its new `expires_at` beside the secret.
- Rotate refuses a revoked or an expired account with `409`, leaving it unchanged.
- The service accounts list has an Expires column with the date and time remaining, and flags an account expiring within 14 days. The secret card shows the expiry after a create or a rotate. The console offers Rotate only on active accounts.

## Impact

- `server/identity/internal/serviceaccounts/store.go`, `server/identity/internal/saadmin/handler.go`, `ui/src/components/ServiceAccounts/`, `docs/service-accounts.md`.
- Not in this change: a signal for accounts about to expire, and per-secret expiry (#1021).
