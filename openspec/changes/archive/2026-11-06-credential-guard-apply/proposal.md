## Why

The server holds the credential guard's policy and pushes it to hosts as `set_credential_guard` (issue #1432), but nothing on the host applies it: the extension still guards its built-in list in detect mode. An operator who switches the guard off, adds a credential or moves to protect mode sees no change on any host.

## What changes

- The agent forwards `set_credential_guard` to the security extension as `credential_guard.update`, checking only the envelope, and holds a policy that arrives before the extension connects until it does, as it holds a watched-path set.
- The extension enforces the newest policy it has been pushed, persisted so a restart enforces it from its first open: its mode (detect, protect or off) and its credentials replace the built-in ones. Off unmutes every credential. A policy with a mode the extension does not know is turned away, and a credential it cannot guard is skipped.
- In protect mode the extension refuses an agent's read of a credential that is not its tool's and reports it as `agent_credential_block`, which the `agent_credential_read` rule alerts as a read the agent was stopped from.

## Impact

- Agent: `agent/commander`, `agent/receiver` (the dispatcher's hold-and-replay now covers both pushes), the Windows sensor (unsupported).
- Extension: `CredentialGuard.swift`, `CredentialGuardSubscriber.swift`, `WatchedPaths.swift` (the persisted store is shared), `XPCEventServer.swift`, `main.swift`.
- Server: the `agent_credential_block` event type in `schema/events.json`, the ingest metrics and the OpenAPI enum; the rule reads both event types.
- Exercised on a live macOS VM (edr-dev).
