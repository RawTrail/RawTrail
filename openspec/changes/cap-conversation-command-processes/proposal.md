## Why

A page of a session's commands lists a command of more than 100 processes in part (issue #1443), but a page of its conversation, which the Timeline reads, listed every process each command started. On dogfood (v0.9.0-rc.5) one page of a long session's conversation was 20 MB, 19 MB of it the commands' process forests (issue #1550). Applied to a saved copy of that page, the same cut takes it to about 10.7 MB.

## What changes

- A conversation page cuts each turn's commands as a page of commands does: its first 100 processes, every alerted one and every one the page's search finds, marked `processes_cut`, with `process_ids` whole.
- The Timeline offers "Show all N processes" for a cut command, as the Commands view does, and now lists the command whole once that read lands. It already showed the offer, but never listed the whole read, since no conversation command was cut before.

## Impact

- `server/detection/internal/operator`: the commands page's cut is shared with the conversation handler.
- `ui/src/components/Sessions/SessionView.tsx`: the Timeline takes a command read whole from the same store the Commands view does.
