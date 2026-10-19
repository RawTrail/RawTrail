## 1. Server, UI, packaging and docs (#1366)

- [x] 1.1 Rename the product to RawTrail in the UI, API reference, rule credits, Navigator layer and docs
- [x] 1.2 Rename the server binaries, images, compose services and the OTel service name
- [x] 1.3 Point the SigNoz dashboards and alert rules at `rawtrail-server`
- [x] 1.4 Send `rawtrail-webhook/1` as the webhook User-Agent
- [x] 1.5 Move the break-glass admin to `admin@rawtrail.local` with an identity migration
- [x] 1.6 Re-credit existing alerts from "Fleet EDR" to "RawTrail" with a batched boot-time pass

## 2. Agent, extensions and installer (#1367)

- [x] 2.1 Rename the agent binary, paths, launchd label, OTel service name and code-signing identifier
- [x] 2.2 Rename the host app, the extensions' bundle identifiers, app group, XPC services and display names
- [x] 2.3 Rename the package, its scripts, the activation LaunchAgent and the configuration profiles
- [x] 2.4 Rename the control channel's gRPC package to `rawtrail.control.v1`
- [ ] 2.5 Replace the three Developer ID provisioning profiles with ones issued for the `io.rawtrail` App IDs before the release build

## 3. Fleet as one MDM among others (#1368)

- [x] 3.1 Fold the Fleet guide into the MDM deployment guide as one vendor section beside the others
- [x] 3.2 Remove Fleet branding and any special relationship from the README, operator docs, comments and the project context, and note the rename in ADR-0003

## 4. Release archive

- [ ] 4.1 Reword the Purpose sections that still say "Fleet EDR" (`endpoint-event-collection`, `server-admin-surface`, `server-availability`, `web-ui`), and the `server-admin-surface` ATT&CK coverage requirement's "Fleet EDR is a macOS-only product" aside, in the release archive, since a delta cannot modify a Purpose
