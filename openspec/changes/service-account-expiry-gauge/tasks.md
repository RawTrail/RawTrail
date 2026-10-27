## 1. Server

- [x] 1.1 `Store.CountExpiring` counts active, unexpired accounts expiring within a window, exposed as `CountExpiringServiceAccounts` on the identity `api.Service`.
- [x] 1.2 The metrics recorder registers `edr.service_accounts.expiring` with a 14 day window, fed by `serverGaugeSource`.
- [x] 1.3 Correct the migration comment about which roles are bindable.

## 2. Docs

- [x] 2.1 `docs/operations.md` and `docs/install-server.md` list the gauge.
