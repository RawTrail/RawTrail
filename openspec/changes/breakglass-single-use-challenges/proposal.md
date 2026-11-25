## Why

A break-glass WebAuthn challenge rides a signed cookie with no server-side row (#1618). Since #1617 it expires five minutes after it is issued, but within those minutes a captured challenge cookie and assertion could be submitted again to mint another session. Passkeys from Apple and most platform authenticators report a sign count of 0, so the sign-count regression check does not catch the repeat. A laptop's passkey-only sign-in needs nothing else. Copilot and CodeRabbit both raised it in review of #1617.

## What Changes

- **Migration `00011_breakglass_consumed_challenges`:** a new table keyed on the challenge's SHA-256, with its expiry as a UTC `DATETIME`. Expiry is compared with `UTC_TIMESTAMP(6)`, so no time zone enters it.
- **Single use:** the password sign-in, the reauth (both through `VerifyLogin`) and the passkey-only sign-in record the challenge once the assertion validates. An insert that collides is refused as `ErrChallengeReused`, audited as `webauthn.challenge_reused`.
- **Sweep:** the identity cleanup timer deletes records an hour past their expiry. The grace outlasts any in-flight consume, so a record cannot vanish while a replay of its challenge is being judged.
- **Setup is unchanged:** its bootstrap token is already single-use, redeemed in the same transaction.
- **Shared helper:** `server/sqlhelpers.IsDuplicateKeyErr` is added for the duplicate-key check, rather than a sixth private copy.

## Impact

- `server/identity/migrations/00011_breakglass_consumed_challenges.sql` (new table, tier 1 under ADR-0009).
- `server/identity/internal/breakglass` (`challenges.go`, `service.go`, `handler.go`) and `server/identity/bootstrap` (the cleanup).
- `server/sqlhelpers/duplicate.go`, and `arch-go.yml` allows identity internals to use `sqlhelpers`.
- `docs/breakglass.md`.
