## Why

The session view reads up to 5000 of a session's processes in one go and groups them into commands, pairs each with its tool call, and searches them in the browser (#1423). A long session sends the whole read to the browser on every open, and paging it there would split commands and change which call pairs with which command at a page boundary.

## What changes

- New `GET /api/hosts/{host_id}/agent-sessions/{session_id}/commands`: the session's commands in the order they started, a page at a time by an opaque cursor, with the matched total. Each command carries its label (the command the agent ran), its processes as a forest with what launchd started for it moved under it, and the Bash call it pairs with or the tied calls it could be.
- `q` keeps the commands whose label, or any of whose processes' path or command line, holds the text, case-insensitively.
- The server groups and pairs over the session's whole process read, so a page boundary changes neither.
- The session summary counts the session's commands, as the commands list lists them.

## Impact

- `server/detection/internal/sessioncommands`: grouping, labelling, launch placement, tool-call pairing, search and paging, moved from the browser.
- `server/detection/internal/operator`: the commands route.
- Not changed: the UI, which adopts the endpoint in #1425; the 5000-process read bound, which #1424 lifts.
