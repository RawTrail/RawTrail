## Why

The event archive kept raw events for a fixed 30 days set in its ClickHouse migration. An operator who wanted a shorter window for a small disk (a laptop install) or a longer one for investigations had to edit a migration. Issue #1434.

## What changes

- New server setting `EDR_EVENT_RETENTION_DAYS` (default 30, 1 to 36500), separate from `EDR_RETENTION_DAYS`.
- At startup, after the ClickHouse migrations, the server sets the events table TTL to the setting when it differs, and issues no ALTER when it already matches.
- Operator docs describe the setting instead of the migration edit.

## Impact

- `server/config`, `server/visibility/bootstrap`, `server/visibility/internal/clickhouse`, both server binaries.
- No migration. A deployment on the migration's 30 days keeps them. A TTL an operator changed by hand is reset to the setting, 30 days when unset, on the first upgraded startup; the changelog's upgrade note says to set it first.
