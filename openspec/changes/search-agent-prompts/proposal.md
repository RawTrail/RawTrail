## Why

The prompts people type to AI agents can be read one session at a time, but nothing finds the session where someone asked an agent to "deploy to prod" or "rotate the keys". Issue #1428, server half.

## What changes

- `GET /api/agent-prompts?q=` finds prompts by their text across every host's sessions, or one host's, newest first, a page at a time. Each result names its host and session.
- A search is bounded: a window of at most 30 days, a week by default, and a query of 3 characters to 255 bytes. It requires `agent_prompt.read` and is audited with its query.
- The event archive keeps each prompt's text lowercased in a column with an ngram skip index, which a search reads with `LIKE`, so it passes over granules that cannot hold the query.

## Impact

- `server/visibility` (ClickHouse migration `00005`, the search read), `server/detection/internal/operator` (the endpoint), `docs/api/openapi.yaml`.
