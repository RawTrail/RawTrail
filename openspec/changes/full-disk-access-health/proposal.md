## Why

A missing Full Disk Access grant is the commonest reason a fresh install captures nothing, and nothing says so: the security extension's `es_new_client` is refused, it exits and relaunches in a loop, and the host reads only "Security extension not activated". The first-run checklist needs to show the missing grant as its own red step (issue #1436).

## What changes

- The security extension records why macOS refused it an Endpoint Security client in `/var/db/io.rawtrail/es-client-status.json` before it exits, and removes the record once a client is granted.
- The agent reads that record, and reports a `not_permitted` refusal as the security extension's condition `full_disk_access_missing`, kept through reconnect attempts and cleared by the next connect.
- The agent checks its own grant by opening the system privacy database, which macOS refuses even root without Full Disk Access, and reports it as the new `agent_full_disk_access` component.
- The host page labels the new component, and the manual install guide says where both conditions show.

## Impact

- Extension: `ESClientStatus.swift`, the two fatal ES clients.
- Agent: `agent/fda`, the health registry, a darwin-only watcher started with the macOS sensors.
- UI: one label on the host page. Server: none, since component types and reasons are open vocabularies.
- Needs live QA on the edr-dev VM: revoke Full Disk Access and see both conditions.
