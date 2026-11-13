## 1. Config

- [x] 1.1 Size ClickHouse's background pools, merge block size and logging for a laptop in `packaging/laptop/clickhouse-laptop.xml`.
- [x] 1.2 Pin the settings in `test/arch/laptop_test.go`.

## 2. Verify

- [x] 2.1 Replay a real laptop's events into the current and the tuned config at the same insert pace and compare memory.
- [ ] 2.2 Recreate ClickHouse on the laptop install and check its memory and the agent's uploads.
