-- +goose Up
-- Stamp grouping keys on each process row at write time (issue #1281, epic #1275), so a reader lists an AI agent's session, or a
-- process's whole story, by an indexed equality lookup instead of walking parent links.
--
-- agent_session_id, agent_name and agent_version identify the AI agent session a process belongs to: an exec that carries the agent's
-- session marker stamps them, and every other row inherits its creator's, so a child whose environment was stripped (sudo, env -i)
-- keeps its session. story_root_id is the row of the nearest ancestor that launchd started, and tool_call_group is the process group
-- of the agent's direct child that started the process; both are stamped by a later change of #1281.
--
-- The columns are added in one INSTANT step. The two indexes are added separately as an online (INPLACE, LOCK=NONE) build, which
-- MySQL 8.4 cannot do instantly; the table stays readable and writable while it builds.
-- No down section, per ADR-0009.

-- +goose StatementBegin
ALTER TABLE processes
	ADD COLUMN agent_session_id VARCHAR(256) NULL,
	ADD COLUMN agent_name VARCHAR(256) NULL,
	ADD COLUMN agent_version VARCHAR(256) NULL,
	ADD COLUMN story_root_id BIGINT NULL,
	ADD COLUMN tool_call_group INT NULL,
	ALGORITHM=INSTANT;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE processes
	ADD INDEX idx_processes_host_agent_session (host_id, agent_session_id),
	ADD INDEX idx_processes_host_story_root (host_id, story_root_id),
	ALGORITHM=INPLACE, LOCK=NONE;
-- +goose StatementEnd
