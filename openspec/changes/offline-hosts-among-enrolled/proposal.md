## Why

`edr.offline.hosts` counted every host detection had ever seen that was now silent, while `edr.enrolled.hosts` counts non-revoked enrollments. A revoked host stops checking in and counted as offline forever without being enrolled. On the dogfood server "EDR hosts offline" (offline over enrolled) read 200% around the clock, and "EDR event ingest stopped", which reads enrolled minus offline as the online count, could never fire once revoked hosts matched the online ones (#1624).

## What Changes

- **Gauge:** `edr.offline.hosts` counts the non-revoked enrollments not seen within the offline threshold, so it is a subset of `edr.enrolled.hosts`. An enrollment that never checked in counts as offline. The server intersects endpoint's active host IDs with the hosts detection saw within the threshold, since detection holds no revocation state. Both gauges are observed by one callback from one read of the enrollments, so a host enrolling mid-collection cannot put offline above enrolled, and host IDs compare case-insensitively, as both contexts' columns do.
- **Detection API:** `CountOfflineHosts` is replaced by `OnlineHostIDs`.
- **Metrics:** the gauge source's `EnrolledHosts` and `OfflineHosts` are replaced by one `HostFleet`.
- **Spec:** MODIFIED "Observable host-fleet gauges" (`observability-instrumentation`).
- **Docs:** the gauge's description, `docs/operations.md` and `docs/install-server.md` say which hosts it counts.

## Impact

- `server/cmd/rawtrail-server/main.go` (the gauge adapter), `server/detection` (store, service, API), `server/metrics`.
- No alert rule changes: both rules read correctly once offline is a subset of enrolled.
