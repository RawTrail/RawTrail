## Why

Pairing a session's commands with its tool calls was quadratic in the size of a tie (issue #1485). With 20,000 identical Bash calls in one turn, the most a session read takes, and 20,500 commands that ran the same text, the join took about 31 seconds, past the server's 30-second write timeout, so the session's commands page could not be served. A transcript is its user's to write, so anyone who can run Claude Code on an enrolled Mac can produce that input.

## What changes

- A scan that finds a time group whose calls all pass returns the list's own slice of them rather than a copy, and the slice is kept for the next command until a call of that group is pinned; a scan that found nothing is kept, since a later command can only find less; a whole tie and a cut tie found at one time are joined once.
- A tie is found by the slice its commands share rather than by naming every call again, and no copy of a tie is kept, so memory stays linear too.
- The labels are unchanged.

## Impact

- `server/detection/internal/sessioncommands/tooljoin.go`. The four shapes a transcript's author could make quadratic join in 5 to 40 ms, with about 20 to 36 MB allocated, where they took 7 to 31 seconds and 67 to 343 GB.
