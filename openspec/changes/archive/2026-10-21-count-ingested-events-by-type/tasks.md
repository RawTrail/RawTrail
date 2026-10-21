## 1. Server

- [x] 1.1 The ingest handler records `edr.events.ingested` once per event type in an accepted batch
- [x] 1.2 The recorder maps an event type outside the schema to `other`
- [x] 1.3 Tests: the attribute value for schema and unknown types; the server's set equals the schema's enum
- [x] 1.4 Docs: the metric list in `docs/install-server.md` names the new attribute
- [x] 1.5 Live: SigNoz shows `edr.events.ingested` grouped by `event_type` for a dev host that sent several types
