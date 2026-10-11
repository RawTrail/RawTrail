## Why

Stopping the server cancels every detection worker's in-flight query, and the processor logged each of those cancellations at ERROR, like a real database failure (issue #1258). Every restart of a replica draining a backlog therefore added ERROR lines, which bury real errors and would trip any alert on ERROR logs.

## What changes

- A processor queue, lock or store call that fails because the worker's own context was cancelled is logged at DEBUG.
- Any other failure stays an ERROR, including a cancellation while the worker is still running, such as a lost host lock cancelling the locked section.

## Not changed

What the processor does after each failure is unchanged: the same returns, nacks and retries run as before. Only the log level of a shutdown-caused failure moves.
