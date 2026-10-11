-- +goose Up
-- Drop principals.disabled_at (issue #1224). It was meant to tombstone a removed user or service account, but nothing ever wrote or
-- read it. An identity's status lives on its own row, where every authentication check already reads it: users.status for a user,
-- service_accounts.revoked_at for a service account. A second copy that no check reads is a trap: setting it would look like
-- disabling the account and would not. Attribution does not need it either. A principals row is never deleted, because foreign keys
-- and the audit trail reference it, so its id and label stay resolvable without a tombstone.
--
-- A drop is a tier-2 change under ADR-0009, which normally needs expand and contract. This one ships in a single step because no
-- binary, this one or any earlier one, reads or writes the column, so a replica still on the previous release is unaffected.

-- +goose StatementBegin
ALTER TABLE principals DROP COLUMN disabled_at;
-- +goose StatementEnd

-- +goose Down
-- Forward-only migrations (ADR-0009).
