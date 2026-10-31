-- +goose Up
-- A bypass sighting records the uid of the transcript it came from (issue #1478), so the sessions list counts a session as bypassed
-- only from its owner's transcript: another local user can write a transcript naming someone else's session. The rows recorded
-- before this migration have no uid and take -1, which no owner has, so they mark nothing; they were written by builds of the same
-- unreleased version, and the next bypass line their sessions write records them again.
-- No down section, per ADR-0009.

-- +goose StatementBegin
ALTER TABLE agent_session_bypass
	ADD COLUMN uid BIGINT NOT NULL DEFAULT -1,
	DROP PRIMARY KEY,
	ADD PRIMARY KEY (host_id, agent_session_id, uid);
-- +goose StatementEnd
