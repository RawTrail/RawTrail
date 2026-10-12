-- +goose Up
-- Store each process's lineage identity on its row (issue #1280, epic #1275), so the process tree can follow a process to the row that
-- created it instead of inferring a parent from ppid and time, which misattributes under pid reuse and breaks once a daemon is
-- reparented to launchd.
--
-- parent_process_id is the id of the creator's row. The graph builder resolves it from the fork event's exact (parent_pid,
-- parent_pidversion). parent_pidversion is stored as well, so a child whose parent row arrives later can still be matched to it
-- exactly, and so is every field the exec event reports: original_ppid (the creator's pid, which survives reparenting), the
-- responsible process, the POSIX session and process group, the controlling terminal, the process start time and the working
-- directory. reparented is true when launchd adopted the process (ppid 1, original_ppid another process).
--
-- Every column is nullable or defaulted, and no index or foreign key is added, so the ALTER is an INSTANT metadata change on MySQL
-- 8.4 even on a large table. There is deliberately no foreign key on parent_process_id: retention prunes exited parents while their
-- children live on. A reader treats a link to a pruned row as the top of the chain.
--
-- Rolling upgrade: an older replica keeps inserting rows with these columns NULL and reparented FALSE, which is exactly what a row
-- from an agent without the lineage fields gets, and the process tree falls back to the ppid rule for such rows.
-- No down section, per ADR-0009.

-- +goose StatementBegin
ALTER TABLE processes
	ADD COLUMN parent_process_id BIGINT NULL,
	ADD COLUMN parent_pidversion INT UNSIGNED NULL,
	ADD COLUMN original_ppid INT NULL,
	ADD COLUMN responsible_pid INT NULL,
	ADD COLUMN responsible_pidversion INT UNSIGNED NULL,
	ADD COLUMN session_id INT NULL,
	ADD COLUMN group_id INT NULL,
	ADD COLUMN tty VARCHAR(255) NULL,
	ADD COLUMN start_time_ns BIGINT NULL,
	ADD COLUMN cwd TEXT NULL,
	ADD COLUMN reparented BOOL NOT NULL DEFAULT FALSE,
	ALGORITHM=INSTANT;
-- +goose StatementEnd
