## Why

The product is renamed from Fleet EDR to RawTrail (epic #1369). It is a standalone product, and Fleet (the MDM) is one deployment channel among several, so its name should not carry another vendor's brand.

## What changes

- The web UI, the API reference, rule credits, the ATT&CK Navigator layer, webhook deliveries and the server's telemetry all name the product RawTrail.
- Server binaries are `rawtrail-server`, `rawtrail-ingest`, `rawtrail-migrate` and `rawtrail-demo-seed`. The release publishes the server and demo-seed images as `ghcr.io/rawtrail/rawtrail-server` and `ghcr.io/rawtrail/rawtrail-demo-seed`.
- The server's OpenTelemetry `service.name` is `rawtrail-server`; the shipped SigNoz dashboards and alert rules filter on it.
- Webhook deliveries send `User-Agent: rawtrail-webhook/1`.
- The break-glass admin is `admin@rawtrail.local`; a migration moves an existing break-glass account, with its sign-in identity, from `admin@fleet-edr.local`.
- Existing alerts credited "Fleet EDR" are re-credited "RawTrail" by a batched one-time pass on the first start after the upgrade.
- The agent is `rawtrail-agent` at `/usr/local/bin/rawtrail-agent`, configured from `/etc/rawtrail.conf` with state under `/var/db/rawtrail` (`%ProgramData%\RawTrail` on Windows), under the launchd label `io.rawtrail.agent`. Its code-signing identifier, which the extensions' XPC peer check pins, is `rawtrail-agent`.
- The host app is `RawTrail.app` (`io.rawtrail`); the extensions are `io.rawtrail.securityextension` and `io.rawtrail.networkextension`, sharing the app group `group.io.rawtrail.networkextension`, and display as "RawTrail Security Extension" and "RawTrail Network Extension". Extension state lives under `/var/db/io.rawtrail`.
- The installer is `rawtrail-<version>.pkg`, with the `io.rawtrail.activate` LaunchAgent and `io.rawtrail.profile.*` configuration profiles. The control channel's gRPC package is `rawtrail.control.v1`.
- Clean break: the new identifiers share nothing with Fleet EDR, so an existing install is uninstalled first and the host re-enrolls.

## Not changed

- Repository-bound names (the GitHub URL, the SonarCloud and Codecov projects) stay until the repository moves.
- Alerts credited to an upstream author keep their credit.
