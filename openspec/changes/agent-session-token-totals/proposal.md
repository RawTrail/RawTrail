## Why

The agent sends what each assistant message used (#1455), but the server reported none of it, so a session could not say how many tokens it took, on which models, or in which permission mode. Issue #1427, server half, first part.

## What changes

- The session summary (`GET .../agent-sessions/{session_id}/summary`) gains `usage`: the session's tokens by model, and its messages by the permission mode in force with the span each was seen over.
- Each message is counted once by its id and owner, at its first recording. A message a forked session copied from the session it names, which that session holds, is that session's and is not counted, by the rule that already leaves out copied tool calls. That rule is now one function of the event type, shared by both.

## Impact

- `server/visibility` (the archive read, the shared copy filter), `server/detection/internal/operator` (the summary), `docs/api/openapi.yaml`.
- The sessions list's token total and bypass-mode filter, the header and the cost estimate follow under #1427.
