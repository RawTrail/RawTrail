-- +goose Up
-- One row per AI agent session seen on a host (issue #1281, epic #1275), so the sessions list is a read of this table rather than a
-- GROUP BY over processes. The graph builder upserts it from every exec that carries the agent's session marker: agent_name and
-- agent_version from that exec's AI_AGENT, agent_pid from its CLAUDE_PID, and cwd from the earliest tool call seen, which is where
-- the session was working when it started. first_seen_ns and last_seen_ns only move outward, so a replayed batch changes nothing.
-- first_event_id is the earliest sighting's event, which breaks a tie between sightings stamped at the same nanosecond, so the row
-- does not depend on which arrived first.
-- The processes of a session are the indexed (host_id, agent_session_id) lookup on processes from migration 00019.
--
-- No foreign key to processes: a session outlives the retention of its earliest process rows.

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS agent_sessions (
	host_id          VARCHAR(255) NOT NULL,
	agent_session_id VARCHAR(256) NOT NULL,
	agent_name       VARCHAR(256) NOT NULL,
	agent_version    VARCHAR(256) NULL,
	agent_pid        INT          NULL,
	cwd              TEXT         NULL,
	first_seen_ns    BIGINT       NOT NULL,
	first_event_id   VARCHAR(255) NOT NULL,
	last_seen_ns     BIGINT       NOT NULL,
	PRIMARY KEY (host_id, agent_session_id),
	INDEX idx_agent_sessions_last_seen (last_seen_ns)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
-- +goose StatementEnd

-- +goose Down
-- Forward-only migrations (ADR-0009). The rollback path is restore-from-backup.
