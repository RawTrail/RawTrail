## Why

`fleet-edr-ingest`, the standalone ingest binary, ignored two settings `fleet-edr-server` honors (issue #1248). Behind a load balancer it logged every agent at the proxy's address, because it ignored `EDR_TRUSTED_PROXIES`. On SIGTERM it closed its listener at once, because it ignored `EDR_SHUTDOWN_DRAIN`, so a rolling restart refused in-flight agent uploads. The binary is not released yet, and it should reach parity before it is.

## What changes

- Both binaries build their HTTP server through one shared function, so the ingest binary resolves a forwarded client address from a trusted proxy exactly as the server does. It also emits HSTS behind a TLS-terminating proxy, as the server does.
- Both binaries overwrite the request span's `client.address` with the resolved address. otelhttp filled it from `X-Forwarded-For` with no trust check, so any client chose the address its spans recorded.
- The ingest binary wires a drain state into its readiness probe and its shutdown: on SIGTERM `/readyz` reports 503 for the drain window before the listener closes.

## Not changed

Apart from the span address, the server's behavior is unchanged. Both settings are parsed by the same config code as before.
