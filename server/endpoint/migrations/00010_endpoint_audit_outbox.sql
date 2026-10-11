-- +goose Up
-- A transactional outbox for the audit rows that record this context's operator actions (issue #1225), starting with revoking a host's
-- enrollment.
--
-- The revoke wrote its audit row after its UPDATE committed and logged rather than returned a failure to write it, so a failed audit
-- write or a crash between the two left a host's credentials revoked with nothing recording who did it or why. The audit store belongs
-- to the identity context and cannot join this context's transaction, so the revoke's transaction writes an entry here instead, which
-- commits if and only if the revoke does, and the shared drain (server/auditoutbox) turns it into an audit row. The response and rules
-- contexts do the same with their own outbox tables, in the same encoding.
--
-- held_until is unused here: a revoke knows its whole audit payload before it commits. The column is part of the shape because the
-- shared drain reads every outbox through one query.
--
-- No secondary index: the table is empty in the steady state, and the drain reads it by primary key order.

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS endpoint_audit_outbox (
	id         BIGINT       NOT NULL AUTO_INCREMENT,
	kind       VARCHAR(64)  NOT NULL,
	payload    JSON         NOT NULL,
	created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
	held_until TIMESTAMP(6) NULL,
	PRIMARY KEY (id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
-- +goose StatementEnd

-- +goose Down
-- Forward-only migrations (ADR-0009). Dropping this would discard audit entries that have not been delivered yet.
