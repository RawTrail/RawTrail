## Why

When more rows than the 64-event budget differ only by process, folding them into parent directories reduces nothing: rows of different processes never merge. The fold kept climbing until every row named a top-level directory such as `/private`, so the window lost every real path. A pipeline does this on its own, since each of its processes closes the output file it inherited as a modified file.

## What changes

- A row the fold merged with nothing keeps the exact path it had.
- A row that merged several paths names the deepest directory they share, not the directory the fold climbed to.
- The budget and the dropped count are unchanged: past 64 rows, the rows standing for the fewest operations are still dropped and logged.

## Impact

- `extension/edr/extension/AgentFileActivity.swift` (`FileActivityRollup.fold`).
- No wire change: `file_activity` rows keep their shape.
