## Why

The session view's Summary (#1429) describes a session at a glance: what it was asked, what it ran, what it touched and where it talked to. The summary endpoint counted those things but did not describe them, so a Summary built on it could say "41 files" but not which directories, or "12 lookups" but not to whom. Issue #1429, server half.

## What changes

- The session summary (`GET .../agent-sessions/{session_id}/summary`) gains `digest`:
  - `prompts`: how many prompts the session was sent;
  - `programs`: the ten programs its commands ran most, the agent's wrapper shell left out;
  - `directories`: the twenty directories with the most file events, with their events by operation and distinct paths, and `outside_project`, the distinct paths outside the session's project;
  - `domains`: the fifty registrable domains (by the public suffix list) with the most lookups and named connections, each marked `new` when the host had not looked it up before the session began;
  - `timeline` and `command_timeline`: the session's events and the commands that started, in 48 equal buckets of a span that holds them all: the session's sightings, widened to its commands and its events.
- The digest reads the same scope the summary's activity totals do.

## Impact

- `server/visibility` (the digest and prompt count reads), `server/detection/internal/operator` (the summary), `docs/api/openapi.yaml`.
- The Summary tab that renders it follows under #1429.
