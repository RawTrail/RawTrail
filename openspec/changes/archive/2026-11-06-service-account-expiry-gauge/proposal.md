## Why

A service account's credential expires without warning: the integration using it starts failing at the token endpoint on the expiry date and nothing signalled it beforehand (#1019). The console now shows the expiry, but a deployment that does not watch the console has no way to alert on it.

## What changes

- A new observable gauge, `edr.service_accounts.expiring`, counts the active service accounts whose credential expires within 14 days, the same window as the console's expiring-soon badge. An account that is revoked or already expired is not counted.
- The stale comment in the service accounts migration, which said `admin` could not be bound, now says only `super_admin` cannot.

## Impact

- `server/metrics/metrics.go`, `server/identity/api/service.go`, `server/identity/internal/serviceaccounts/store.go`, `server/cmd/rawtrail-server/main.go`, `docs/operations.md`, `docs/install-server.md`.
- OTel only: no Prometheus endpoint, and no notification beyond the metric.
