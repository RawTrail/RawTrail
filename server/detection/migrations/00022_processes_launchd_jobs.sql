-- +goose Up
-- Store the launchd job definitions a `launchctl load|bootstrap` exec loaded (issue #1341, epic #1343): each plist's path, label,
-- program and program arguments, as the agent read them. The process tree links the command to the job launchd starts by the
-- label, and a persistence detection reads the program.
--
-- One nullable JSON column, NULL for every other exec, with no index or default, so the ALTER is an INSTANT metadata change on
-- MySQL 8.4 even on a large table. Rolling upgrade: an older replica inserts rows with it NULL, which is what a row from an agent
-- without the field gets, and the tree simply links no load or bootstrap for such rows.
-- No down section, per ADR-0009.

-- +goose StatementBegin
ALTER TABLE processes ADD COLUMN launchd_jobs JSON NULL, ALGORITHM=INSTANT;
-- +goose StatementEnd
