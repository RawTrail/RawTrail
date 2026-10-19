-- +goose Up
-- Rename the break-glass admin's well-known email from admin@fleet-edr.local to admin@rawtrail.local, with the product rename
-- (#1366). The seed looks the account up by that email on every boot, so without this an upgraded server would find no
-- break-glass row at the new address, create a second super_admin break-glass account beside the old one, and print a fresh
-- redemption URL for it. The principal's display label is a snapshot of the email and moves with it.
--
-- A data rewrite, not a schema change, so ADR-0009's expand and contract does not apply. A replica still on the previous release
-- only reads the email at boot, and a rolling upgrade restarts every replica onto the new binary.

-- +goose StatementBegin
UPDATE users SET email = 'admin@rawtrail.local' WHERE email = 'admin@fleet-edr.local' AND is_breakglass = 1;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE principals p JOIN users u ON p.id = CONCAT('usr_', u.id)
SET p.display_label = u.email
WHERE u.email = 'admin@rawtrail.local' AND u.is_breakglass = 1;
-- +goose StatementEnd

-- +goose Down
-- Forward-only migrations (ADR-0009).
