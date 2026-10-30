-- +goose Up
-- Searching prompt text across sessions (issue #1428): materialize each agent_prompt's text, lowercased, as prompt_text, and give it
-- an ngram bloom-filter skip index, so a search for a substring passes over granules whose trigrams cannot hold it. Other events
-- carry an empty prompt_text. A search filters on event_type and a bounded window as well, and reads prompt_text with LIKE, the
-- predicate the index serves. ngrambf_v1(3, ...) rather than tokenbf_v1: a search is for text inside a prompt, not whole words.
-- MATERIALIZE backfills the column and index over parts written before this migration, as migration 00004 does.

-- +goose StatementBegin
ALTER TABLE events ADD COLUMN IF NOT EXISTS prompt_text String
	MATERIALIZED if(event_type = 'agent_prompt', lowerUTF8(JSONExtractString(payload, 'text')), '');
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE events ADD INDEX IF NOT EXISTS idx_events_prompt_text prompt_text TYPE ngrambf_v1(3, 65536, 3, 0) GRANULARITY 1;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE events MATERIALIZE COLUMN prompt_text;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE events MATERIALIZE INDEX idx_events_prompt_text;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE events DROP INDEX IF EXISTS idx_events_prompt_text;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE events DROP COLUMN IF EXISTS prompt_text;
-- +goose StatementEnd
