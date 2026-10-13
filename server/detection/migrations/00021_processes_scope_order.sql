-- +goose Up
-- Let a story or agent session read stop at its row limit (issue #1282). The tree's scoped read is WHERE host_id = ? AND
-- agent_session_id = ? (or story_root_id = ?) ORDER BY fork_time_ns, id LIMIT ?, and the migration 00019 indexes ended at the scope
-- column, so MySQL read and sorted every row of the scope before the limit applied: work in proportion to a whole story, which under
-- an IDE terminal can be most of a host's history. Appending fork_time_ns makes the index order the read's order (InnoDB appends the
-- primary key, id, to every secondary index), so the scan stops after the lookahead row. The scope count still reads only the index.
--
-- Built online (INPLACE, LOCK=NONE), as 00019's indexes were; the table stays readable and writable while it builds.
-- No down section, per ADR-0009.

-- +goose StatementBegin
ALTER TABLE processes
	DROP INDEX idx_processes_host_agent_session,
	DROP INDEX idx_processes_host_story_root,
	ADD INDEX idx_processes_host_agent_session_fork (host_id, agent_session_id, fork_time_ns),
	ADD INDEX idx_processes_host_story_root_fork (host_id, story_root_id, fork_time_ns),
	ALGORITHM=INPLACE, LOCK=NONE;
-- +goose StatementEnd
