-- +goose Up
-- breakglass_consumed_challenges makes each break-glass WebAuthn challenge single-use (issue #1618). The challenge rides a signed cookie
-- with no server-side row, so before this table a captured challenge and assertion could be submitted again until the challenge expired
-- five minutes after it was issued. Passkeys from Apple and most platform authenticators report a sign count of 0, so the sign-count
-- regression check does not catch the repeat, and a laptop's passkey-only sign-in needs nothing else. Finishing a ceremony inserts the
-- challenge's SHA-256 here; the primary key turns a second insert into a refusal, on every replica (ADR-0010). The identity cleanup loop
-- deletes rows an hour past expires_at; the challenge is refused as expired from expires_at on.
--
-- expires_at is DATETIME, not TIMESTAMP, and holds UTC: it is compared with UTC_TIMESTAMP(6), and DATETIME is stored and compared as
-- written, so neither the MySQL session time zone nor the server's affects the comparison.
--
-- A new table is a tier-1 change under ADR-0009: a replica still on the previous release neither reads nor writes it.

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS breakglass_consumed_challenges (
	challenge_hash BINARY(32)   NOT NULL PRIMARY KEY,
	expires_at     DATETIME(6)  NOT NULL,
	INDEX idx_breakglass_consumed_challenges_expiry (expires_at)
);
-- +goose StatementEnd

-- +goose Down
-- Forward-only migrations (ADR-0009).
