## Why

Edits are the main thing a coding agent does, and file events today cover only the watched sensitive paths. Claude Code's Read, Edit and Write run inside the `claude` process itself, so an operator reviewing a session cannot see which files it created or changed (#1287).

## What changes

- A fourth Endpoint Security client observes file activity (create, modified close, rename, delete) for an AI agent and every process it starts, and for nothing else on the host. It starts from the Claude Code binary or a process carrying a session id and follows forks and execs in the same ordered stream as the file events, so a child that execs and writes at once is still seen.
- A new `file_activity` event carries each process's operations, folded over one second: repeats of one path become one row with a count, and a busy window rolls up into directories so at most 64 events a second leave the host.
- Terminal and other device writes are not reported.

## Impact

- `extension/edr/extension/AgentFileActivity.swift`, `AgentFileSubscriber.swift`, `ESFSubscriber.swift`, `main.swift`.
- `schema/events.json`, the OpenAPI event type enum, and the ingest metrics' type set gain `file_activity`. The server stores the events; reading them into the activity view and the timeline is the next change.
