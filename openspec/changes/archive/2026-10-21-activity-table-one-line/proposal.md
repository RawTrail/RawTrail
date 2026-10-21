## Why

The activity tables (Files, Network and DNS, in the process detail panel, a session's Activity view and the host graph) let a cell break after any character. In the narrow process detail panel, the path column shrank to a few characters per line and could not be read.

## What changes

- Every activity table cell stays on one line. A long path, destination or name reads whole, and the table scrolls sideways within its frame.

## Impact

- `ui/src/components/Activity/ActivityTabs.scss`, and the agent-sessions Playwright journey.
