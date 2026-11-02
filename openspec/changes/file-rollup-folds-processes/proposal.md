## Why

The extension sends at most 64 `file_activity` rows a second. Rows of different processes never merged, so a second with more than 64 of them dropped the rest. A pipeline does this on its own: on edr-dev, an 80-job `xargs` closed one output file from 221 process generations in a second, and 309 of its 441 operations were dropped, with only a line in the system log to show for it. An EDR should aggregate identical events with a count rather than lose them.

## What changes

- Past the budget, rows of one operation on one path fold into the nearest observed process their processes all descend from, keeping the path, with a new `process_count`. Processes fold before directories, since folding them keeps every path.
- The two folds alternate until the window fits; rows are dropped only when neither can fold more, which takes more top-level directories, operations and separate agent subtrees in one second than the budget.
- Rows of two agent sessions never fold together, including two sessions of one Claude Code process after `/clear`: a process's session is the `CLAUDE_CODE_SESSION_ID` its exec carries, kept across forks and execs. A row never names an ancestor that had exited before its first operation.
- The activity API lists a folded row apart from the named process's own operations on the path, with a `processes` count, and the Files tab and the host timeline say how many processes a row counts.

## Impact

- Extension: `AgentProcessSet` keeps each generation's parent; `FileActivityRollup` folds by process.
- Wire: `file_activity` gains the optional `process_count` (`schema/events.json`). Older servers ignore it and read the row as the named process's.
- Server: the file activity list groups folded rows apart and returns `processes`; its page cursor carries the distinction.
- UI: the Files tab and the host timeline.
