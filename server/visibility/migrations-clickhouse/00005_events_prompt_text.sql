-- +goose Up
-- Searching prompt text across sessions (issue #1428): materialize each agent_prompt's text, lowercased, as prompt_text, so a search
-- matches it with LIKE rather than lowercasing every prompt's JSON at read time. Other events carry an empty prompt_text.
--
-- No skip index: a search reads under FINAL, and a skip index used under FINAL can pass over the granule holding an event's newer
-- version, whose text a redelivery changed, and leave its older, matching one standing. The primary key (host_id, event_type,
-- timestamp_ns, event_id) already narrows a search to the agent_prompt rows in its window, at most 30 days of them.
-- MATERIALIZE backfills the column over parts written before this migration, as migration 00004 does.

-- +goose StatementBegin
ALTER TABLE events ADD COLUMN IF NOT EXISTS prompt_text String
	MATERIALIZED if(event_type = 'agent_prompt', lowerUTF8(JSONExtractString(payload, 'text')), '');
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE events MATERIALIZE COLUMN prompt_text;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE events DROP COLUMN IF EXISTS prompt_text;
-- +goose StatementEnd
