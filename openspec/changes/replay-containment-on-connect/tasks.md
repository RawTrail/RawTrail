## 1. Agent

- [x] 1.1 `Manager.Apply` keeps a command whose send found no extension connection, newest by epoch, version and reachable-set version.
- [x] 1.2 `Reconnected` signals the refresh worker, which replays the kept command with a fresh lifeline lookup and waits for the extension's status as Apply does.
- [x] 1.3 Tests: replay once, newest wins in both arrival orders, an overtaken or already-held command is dropped, another failure is not kept, a failed replay is kept.

## 2. QA

- [ ] 2.1 Live macOS VM (tracked in the v0.9.0 RC issue): contain a host whose network extension is not yet approved, approve it, and confirm containment applies within a minute.
