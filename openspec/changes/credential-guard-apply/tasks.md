## 1. Agent

- [x] 1.1 `set_credential_guard` forwards the raw payload over XPC as `credential_guard.update`, after an envelope check
- [x] 1.2 The dispatcher holds the newest policy that found no connection and replays it on connect, beside the watched-path set

## 2. Extension

- [x] 2.1 Decode the pushed policy: unknown mode refused, unknown kind or match or unusable path skipped
- [x] 2.2 Persist and order it as a watched-path set is, through one shared store
- [x] 2.3 Apply its mode and credentials: re-mute on change, off unmutes every credential, protect denies and reports `agent_credential_block`

## 3. Server

- [x] 3.1 `agent_credential_block` in the event schema, ingest metrics and OpenAPI
- [x] 3.2 `agent_credential_read` alerts a refused read as stopped

## 4. Tests and QA

- [x] 4.1 Agent executor and dispatcher tests; Swift decode, store, policy and dispatch tests; rule test and fixture
- [ ] 4.2 edr-dev: push detect, off and protect, and check the guard's events and the denial
