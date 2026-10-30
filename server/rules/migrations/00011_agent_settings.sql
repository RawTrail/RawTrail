-- +goose Up
-- The agent settings (issue #1426): fleet-wide switches for what the agent collects, pushed to every host as one document. One row,
-- replaced whole and versioned as a whole like the watched-path set, so a version names exactly one set of switches.
--
-- settings is a JSON object so a later switch is a new key rather than a migration. A key that is absent reads as that switch's
-- default, which is how the seeded row at version 0 means prompts are captured.
--
-- A new table, so no online-DDL concern.

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS agent_settings (
	id         TINYINT      NOT NULL PRIMARY KEY,
	version    BIGINT       NOT NULL DEFAULT 0,
	settings   JSON         NOT NULL,
	updated_at TIMESTAMP(6) NULL,
	updated_by VARCHAR(255) NOT NULL DEFAULT ''
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT IGNORE INTO agent_settings (id, version, settings) VALUES (1, 0, JSON_OBJECT());
-- +goose StatementEnd
