## 1. Implementation

- [x] 1.1 Parse and bound `EDR_EVENT_RETENTION_DAYS` in the server config
- [x] 1.2 Set the events TTL at startup when it differs from the setting
- [x] 1.3 Pass the setting from rawtrail-server and rawtrail-ingest
- [x] 1.4 Update the operator docs

## 2. Tests

- [x] 2.1 Config parsing: default, override, zero and over-cap rejected
- [x] 2.2 Integration: the setting governs expiry, and an unchanged setting issues no ALTER
