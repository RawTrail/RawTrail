-- +goose Up
-- The AI agent session an alert belongs to, when its rule resolved one that the alert's process does not carry (issue #1589).
--
-- A session's views find its alerts through each alert's process. agent_credential_read groups the agent's own read with its
-- session's alert, and the agent's own process carries no session marker, so an alert whose first read was the agent's would be
-- anchored on a process outside the session and missing from the session's views. NULL for every other alert, whose session is
-- still its process's.
-- No down section, per ADR-0009.

-- +goose StatementBegin
ALTER TABLE alerts
	ADD COLUMN agent_session_id VARCHAR(256) NULL,
	ADD INDEX idx_alerts_host_agent_session (host_id, agent_session_id);
-- +goose StatementEnd
