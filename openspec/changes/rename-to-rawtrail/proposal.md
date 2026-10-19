## Why

The product is renamed from Fleet EDR to RawTrail (epic #1369). It is a standalone product, and Fleet (the MDM) is one deployment channel among several, so its name should not carry another vendor's brand.

## What changes

- The web UI, the API reference, rule credits, the ATT&CK Navigator layer, webhook deliveries and the server's telemetry all name the product RawTrail.
- Server binaries are `rawtrail-server`, `rawtrail-ingest`, `rawtrail-migrate` and `rawtrail-demo-seed`. The release publishes the server and demo-seed images as `ghcr.io/getvictor/rawtrail-server` and `ghcr.io/getvictor/rawtrail-demo-seed`.
- The server's OpenTelemetry `service.name` is `rawtrail-server`; the shipped SigNoz dashboards and alert rules filter on it.
- Webhook deliveries send `User-Agent: rawtrail-webhook/1`.
- The break-glass admin is `admin@rawtrail.local`; a migration moves an existing break-glass account, with its sign-in identity, from `admin@fleet-edr.local`.
- Existing alerts credited "Fleet EDR" are re-credited "RawTrail" by a batched one-time pass on the first start after the upgrade.

## Not changed

- The agent, its macOS extensions and its installer keep their current names, paths and bundle identifiers until #1367.
- Repository-bound names (the GitHub URL, the SonarCloud and Codecov projects) stay until the repository moves.
- Alerts credited to an upstream author keep their credit.
