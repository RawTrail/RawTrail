## 1. Server

- [x] 1.1 The processor records `edr.detection.lag` per event after a successful acknowledgement
- [x] 1.2 `CountPending` stops at 1,000,000; `fleet-edr-server` reports it as `edr.events.pending`
- [x] 1.3 Tests: lag only after an acknowledgement this attempt holds; the count saturates at its cap; the instruments are registered under their names; the ingest tier registers no backlog gauge
- [x] 1.4 Live: lag and backlog show in SigNoz from the lane B dev server, and the capped count stays under the gauge budget on a multi-million-row queue

## 2. Alerts and docs

- [x] 2.1 "EDR detection lag high" and "EDR detection stalled" rules under `config/observability/alerts/`, with tests pinning their queries, thresholds, windows and severities
- [x] 2.2 The operations and install guides list both metrics and both rules
