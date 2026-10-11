-- +goose Up
-- Contract half of 00005_commands_host_status_acked_index (issue #1082). idx_commands_host_status (host_id, status) is the leftmost
-- prefix of idx_commands_host_status_acked (host_id, status, acked_at), so every read it served is served by the wider index, and it
-- now costs a write on every command insert and status change for no read.
--
-- ADR-0009 puts drops in tier 2: the expand shipped in v0.6.0, and every release since reads through the wider index, so no binary a
-- rolling upgrade can still be running depends on this one.
--
-- Dropping a secondary index on InnoDB is metadata-only. The algorithm and lock are stated so MySQL refuses the statement rather than
-- falling back to a plan that blocks command writes. No down section, per the same ADR.

-- +goose StatementBegin
DROP INDEX idx_commands_host_status ON commands ALGORITHM=INPLACE LOCK=NONE;
-- +goose StatementEnd
