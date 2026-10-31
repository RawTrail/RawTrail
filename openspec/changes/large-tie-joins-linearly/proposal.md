## Why

Pairing a session's commands with its tool calls was quadratic in the size of a tie (issue #1485). With 20,000 identical Bash calls in one turn, the most a session read takes, and 20,500 commands that ran the same text, the join took about 31 seconds, past the server's 30-second write timeout, so the session's commands page could not be served. A transcript is its user's to write, so anyone who can run Claude Code on an enrolled Mac can produce that input.

## What changes

- A scan of a list of calls is kept and shared by every command that scans the list from the same place, so a tie's calls are walked and copied once rather than once per command, and the commands share its match without naming every call again.
- The labels are unchanged.

## Impact

- `server/detection/internal/sessioncommands/tooljoin.go`. The 20,000-call case joins in about 40 ms.
