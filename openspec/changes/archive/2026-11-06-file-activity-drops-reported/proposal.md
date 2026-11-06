## Why

When a second of an AI agent session has more file activity than the extension can send, even after folding rows into the processes that started them and into their directories, the rows past the budget are dropped. Until now the only record of that was a line in the Mac's system log, so the console showed the session's file activity as complete when it was not. A sensor should say when its record has a gap.

## What changes

- The extension sends a `file_activity_dropped` event for each agent subtree that lost operations in a second: the topmost observed process of its session, how many operations, and when the first and last happened.
- The session digest counts those operations as `unrecorded`. An event whose operations fall on both sides of a `/clear` in one Claude Code process counts toward both sessions, so neither reads as complete.
- The session Summary says how many file operations were not recorded, and the card it is shared as notes them under its files changed, so neither states a file count as complete when it is not.

## Impact

- Extension: `FileActivityRollup` charges each dropped row to the top of its lineage; `AgentFileSubscriber` sends the events. `AgentProcessSet` moves to its own file.
- Wire: new event type `file_activity_dropped` (`schema/events.json`). An older server stores it and shows nothing for it.
- Server: `FileDigest.unrecorded`.
- UI: the session Summary.
