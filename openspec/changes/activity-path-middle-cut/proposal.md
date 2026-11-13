## Why

The Activity Files table cut each path at its start, so a long path read `…h_refresh.lock.owner.tmp` and lost where the file lives, often the security-relevant part (a home directory or a system one). Its cell was also capped at 36rem, though the shorter timestamps left more room (#1573).

## What changes

- A path is cut in its middle: its start and its last two segments (the parent directory and the name) stay in view, with the ellipsis between them. A name too long for the cell on its own is still cut at its start.
- The path column takes the width the other columns leave, down to a minimum below which the table scrolls sideways.
- The cell's text and tooltip stay the whole path.

## Impact

- `ui/src/components/Activity/` (the session Activity view and the process detail panel).
