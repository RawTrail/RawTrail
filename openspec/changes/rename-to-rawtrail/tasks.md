## 1. Server, UI, packaging and docs (#1366)

- [x] 1.1 Rename the product to RawTrail in the UI, API reference, rule credits, Navigator layer and docs
- [x] 1.2 Rename the server binaries, images, compose services and the OTel service name
- [x] 1.3 Point the SigNoz dashboards and alert rules at `rawtrail-server`
- [x] 1.4 Send `rawtrail-webhook/1` as the webhook User-Agent
- [x] 1.5 Move the break-glass admin to `admin@rawtrail.local` with an identity migration
- [x] 1.6 Re-credit existing alerts from "Fleet EDR" to "RawTrail" with a batched boot-time pass

## 2. Release archive

- [ ] 2.1 Reword the Purpose sections that still say "Fleet EDR" (`endpoint-event-collection`, `server-admin-surface`, `server-availability`, `web-ui`) in the release archive, since a delta cannot modify a Purpose
