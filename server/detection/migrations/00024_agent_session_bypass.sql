-- +goose Up
-- The agent sessions that ran with permissions bypassed (issue #1427), so the sessions list can filter on it without reading the
-- event archive. The graph builder records a session here from any agent_usage event that names it in the bypassPermissions mode,
-- a forked session's copy of its parent's lines included, so a fork of a bypassed session is listed as one. first_seen_ns is the
-- earliest such event and only moves earlier, so a replayed batch changes nothing.
--
-- Its own table rather than a column on agent_sessions: a usage event can be folded before the first exec of its session, which is
-- what creates the session's row.
-- No down section, per ADR-0009.

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS agent_session_bypass (
	host_id          VARCHAR(255) NOT NULL,
	agent_session_id VARCHAR(256) NOT NULL,
	first_seen_ns    BIGINT       NOT NULL,
	PRIMARY KEY (host_id, agent_session_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
-- +goose StatementEnd
