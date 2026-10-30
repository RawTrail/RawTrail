-- +goose Up
-- The credential guard's policy (issue #1432): its mode and the credential files it protects, pushed to every host as one document.
-- One row, replaced whole and versioned as a whole like the agent settings, so a version names exactly one policy.
--
-- policy is a JSON object. The seeded row at version 0 holds none, which reads as the default policy: detect mode over the
-- credentials the extension protects built in.
--
-- A new table, so no online-DDL concern.

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS credential_guard (
	id         TINYINT      NOT NULL PRIMARY KEY,
	version    BIGINT       NOT NULL DEFAULT 0,
	policy     JSON         NOT NULL,
	updated_at TIMESTAMP(6) NULL,
	updated_by VARCHAR(255) NOT NULL DEFAULT ''
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT IGNORE INTO credential_guard (id, version, policy) VALUES (1, 0, JSON_OBJECT());
-- +goose StatementEnd
