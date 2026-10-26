## Why

A host told to contain before its network extension was connected stayed uncontained for up to six hours (#1404). The agent answered the command `failed` with `no connector published`, and the server counts a failed command as delivered for six hours before it sends the state again. Containment is a security control, so the delay is a worse failure than a missing watched path, which #1386 fixed the same way.

## What changes

- The agent keeps the newest containment command that found no extension connection and sends it when the extension connects, building the document afresh so a contained host's lifeline is looked up at that moment. The command still reports `failed`, which is true of that attempt.
- A kept command that the extension already holds or has been overtaken by is dropped, and a replay that fails is kept for the next connection or refresh pass.

## Impact

- `agent/containment/containment.go`.
- The console shows the original command as failed even once the host is contained by the replay, until the server's next catch-up send confirms it. The host is the safer way round.
