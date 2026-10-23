-- +goose Up
-- Index the creator edge so the descendant walk can follow it. The walk (descendantsWhere in internal/mysql/search.go) serves the
-- agent session tree, the session's alert list and process search's descendant filter, and joins each step on parent_process_id,
-- which migration 00018 added without an index. Every level of the walk then scanned the host's whole table: on the dogfood host
-- (5.1M rows) the session tree outlasted the 30 s request deadline. With this index each step is an index range per row it reaches.
--
-- Built online (INPLACE, LOCK=NONE), as 00019's and 00021's indexes were; the table stays readable and writable while it builds.
-- No down section, per ADR-0009.

-- +goose StatementBegin
ALTER TABLE processes
	ADD INDEX idx_processes_host_parent (host_id, parent_process_id),
	ALGORITHM=INPLACE, LOCK=NONE;
-- +goose StatementEnd
