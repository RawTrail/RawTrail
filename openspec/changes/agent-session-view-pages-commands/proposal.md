## Why

The session view read up to 5000 of a session's processes and grouped, paired and searched them in the browser on every open (#1425). The server now groups a session's commands, pairs each with its tool call, totals the session and pages all of it (#1422, #1423, #1424).

## What changes

- The Commands view reads the session's commands from the server a page at a time, with the next page on request.
- A search in the Commands view asks the server for the commands it finds once the operator stops typing.
- The header counts the session's commands and processes from its totals, not from the rows read.
- Each read's cut is told with the view it cuts: the commands' with Commands, the process tree's with Processes.
- An ambiguous tool call names how many identical calls it could be when the server lists only the first of them.

## Impact

- `ui/src/components/Sessions/SessionView.tsx`, `sessionTree.ts`, `ui/src/api.ts`, `ui/src/types.ts`.
- Removed: the browser's grouping (`buildCommands`) and tool-call pairing (`toolCallJoin.ts`), and `getAgentToolCalls`.
- Not changed: the Processes and Activity views, which page in a follow-up.
