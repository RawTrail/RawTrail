## 1. Server

- [x] 1.1 Read a triggering event's envelope from a captured copy before the archive, for alerts and monitor records

## 2. Tests

- [x] 2.1 A replayed finding reads nothing from the archive; another alert reads only the events no alert holds; a monitor record reuses a copy, all mutation-checked
- [ ] 2.2 After the RC is deployed, confirm on dogfood that ClickHouse evidence lookups fall from thousands an hour
