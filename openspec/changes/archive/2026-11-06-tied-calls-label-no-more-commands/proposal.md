## Why

Identical calls of one turn are offered to their commands as a tie, and a tie of n calls stopped being offered once n commands had taken it. Two calls cut to the agent's 8 KiB limit can overlap, one a prefix of the other, and a command that matched only one of them could then take it alone while the tie's earlier command still stood for it: the two calls labelled three commands. Issue #1442.

## What changes

- The join keeps a call of its own for every command it labels. A command takes its nearest calls only when every labelled command can still have a call of its own, and otherwise looks further back, so overlapping ties never label more commands than they hold calls.
- A tie of n identical calls still labels n commands, and a single call still labels one.

## Impact

- `server/detection/internal/sessioncommands/tooljoin.go`.
