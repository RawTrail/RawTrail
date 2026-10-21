## Why

An exit event binds to a process row by pid plus "currently alive", and file events carry only a pid (#1279, epic #1275). Under pid reuse both can land on the wrong process, and neither can be joined exactly to a process row. Exec, fork, network and DNS events already carry `pidversion`. Subtree activity totals (#1285) need an exact actor on every event.

## What Changes

- `exit` payloads carry the exiting process's `pidversion`.
- `open`, `file_rename`, `file_truncate` and `file_delete` payloads carry the acting process's `pidversion`. This covers the file-tamper client and the credential-store `open` events.
- The field is optional and omitted when unknown, so older agents stay valid.
- The server decodes the exit `pidversion`; binding exits by it lands in #1280.

## Impact

- `extension/edr/extension/EventSerializer.swift`, `ESFSubscriber.swift`, `FileTamperSubscriber.swift`, `CredentialStoreSubscriber.swift`, and `schema/events.json`.
- `server/detection/internal/graph/builder.go` (decode only).
