## 1. Server

- [x] 1.1 `bootstrap.NewHTTPServer` builds the HTTP server for both binaries: middleware, trusted-proxy client resolution, timeouts and TLS
- [x] 1.2 `fleet-edr-ingest` wires a `DrainState` into detection's readiness gate and into `RunAndShutdown` with `EDR_SHUTDOWN_DRAIN`
- [x] 1.3 Tests: forwarded-address resolution through `NewHTTPServer`; intake-mode `/readyz` reports 503 once draining
- [x] 1.4 Live: `fleet-edr-ingest` behind a trusted proxy logs the forwarded address, and reports `/readyz` 503 during its drain window
