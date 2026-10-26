## Why

A freshly installed Mac ran for up to six hours without the deployment's watched paths (#1386). The first `set_watched_paths` reaches the agent before the extensions are approved, and the agent answers `failed` with `no connector published`. The server counts a failed command as delivered for six hours before it sends the set again, and approving the extensions takes longer than the five minute sweep on almost every install, so nothing resent the set when the extension connected.

## What changes

- The agent's dispatcher keeps the latest watched-path push that found no connection to the extension and sends it as soon as one is published. The command still reports `failed`, which is the true record of that attempt.

## Impact

- `agent/receiver/loop.go`: `Dispatcher.SendWatchedPaths` and `Dispatcher.Set`.
- Not changed: application control and network containment pushes, which still rely on the server to send them again. Containment is tracked separately.
