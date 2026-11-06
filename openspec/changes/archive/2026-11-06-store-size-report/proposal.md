## Why

A server that captures everything from a few Macs writes several GB a day, and nothing tells its operator how much disk the stores take until the disk is full (issue #1434). Event retention is already a setting (`EDR_EVENT_RETENTION_DAYS`); the operator needs the size beside it to choose it.

## What changes

- `GET /api/v1/admin/storage` reports the space the event archive and the database take, their total, the event retention, and a warning threshold.
- `EDR_STORE_SIZE_WARN_GB` (default 100, 0 never warns) sets the threshold.
- Admin settings > Data collection shows the sizes and the retention, and warns past the threshold with what to do.

## Impact

- Server: config, the visibility archive (`ArchiveBytes`), the detection store and operator handler.
- UI: the Data collection page.
- Docs: install-server, operations, OpenAPI, changelog.
