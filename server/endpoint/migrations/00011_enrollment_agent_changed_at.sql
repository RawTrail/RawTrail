-- +goose Up
-- agent_changed_at is when a host's check-in last reported an agent version other than the one stored (issue #1465): an upgrade in
-- place, which does not re-enroll. A push queued before it reached the agent that was replaced, which may have failed it as a command
-- it did not know, so the catch-ups resend it as they do after a re-enrollment. NULL until a check-in changes the version.
-- No down section, per ADR-0009.

-- +goose StatementBegin
ALTER TABLE enrollments ADD COLUMN agent_changed_at TIMESTAMP(6) NULL;
-- +goose StatementEnd
