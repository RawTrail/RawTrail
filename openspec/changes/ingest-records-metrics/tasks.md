## 1. Server

- [x] 1.1 `fleet-edr-ingest` builds a recorder with no gauge source, sets it on detection, and passes it to its HTTP server
- [x] 1.2 Test: the recorder reaches detection, records `edr.events.ingested`, and registers no host-fleet gauge
- [x] 1.3 Live: an event accepted by `fleet-edr-ingest` shows in `edr.events.ingested` under `service.name = fleet-edr-ingest`
