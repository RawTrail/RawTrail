-- +goose Up
-- The silent-contained-host alert (issue #1228) needs two things the hosts row did not hold.
--
-- last_checkin_ns is when the SERVER last heard from the host, on its own clock: the command poll and the control stream. last_seen_ns
-- cannot stand in for it, because ingest advances that from the timestamps the agent puts on its events, so a host whose clock runs a
-- day ahead could pin it a day into the future and stay "seen" for a day after it stopped reporting.
--
-- silent_alerted_epoch is the containment the host last raised that alert for. The alert's own dedup key stops a second row only while
-- the first exists, and alert retention deletes alerts by age whatever their status, so a host silent for longer than the retention
-- window would otherwise alert again for the same containment.
-- No down section, per ADR-0009.

-- +goose StatementBegin
ALTER TABLE hosts
	ADD COLUMN last_checkin_ns BIGINT NOT NULL DEFAULT 0,
	ADD COLUMN silent_alerted_epoch BIGINT NOT NULL DEFAULT 0;
-- +goose StatementEnd
