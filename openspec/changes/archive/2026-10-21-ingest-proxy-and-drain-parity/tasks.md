## 1. Server

- [x] 1.1 `bootstrap.NewHTTPServer` builds the HTTP server for both binaries: middleware, trusted-proxy client resolution, timeouts and TLS
- [x] 1.2 `rawtrail-ingest` wires a `DrainState` into detection's readiness gate and into `RunAndShutdown` with `EDR_SHUTDOWN_DRAIN`
- [x] 1.3 `httpserver.Build` overwrites the span's `client.address` with the resolved client address
- [x] 1.4 Tests: forwarded-address resolution through `NewHTTPServer`; intake-mode `/readyz` reports 503 once draining
- [x] 1.5 Live: `rawtrail-ingest` behind a trusted proxy logs the forwarded address, and reports `/readyz` 503 during its drain window
