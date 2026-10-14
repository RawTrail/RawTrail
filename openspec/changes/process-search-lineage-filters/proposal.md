## Why

The fleet process search filters by host, path, hash, user, signer and time, but not by what the epic made the unit of an investigation: an AI agent's session, a story, or everything a process started. "Every `curl` any Claude Code session ran this week" took several searches and a manual join (#1289).

## What changes

- `GET /api/search/processes` gains `agent` (any session of an agent, by the name its `AI_AGENT` marker gives it), `agent_session` (with `host_id`, since a session id names a session only on its host), `story_root`, `ancestor` (the descendants of a process: what it forked, what they forked, and its re-execs) and `args` (a substring of one argument, case-sensitive).
- A `story_root` or `ancestor` that is not a positive process id is refused with `invalid_process`, rather than dropped into a fleet-wide search.
- The search page gains Agent, Agent session, Story root, Descendant of, Argument and Since chips. Since reaches back an hour, a day, a week or 30 days from when the search ran.

## Impact

- `server/detection/internal/mysql/search.go`, `server/detection/internal/operator/process_search_handler.go`, `server/detection/api/types.go`.
- `ui/src/components/Search/ProcessSearch.tsx`, a new `processSearchFilter.ts`, `ui/src/api.ts`, and their tests.
