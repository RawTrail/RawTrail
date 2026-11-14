## 1. Config

- [x] 1.1 Run the laptop ClickHouse's allocator with one arena in `docker-compose.laptop.yml`.
- [x] 1.2 Pin the setting in `test/arch/laptop_test.go`.

## 2. Verify

- [x] 2.1 Replay a burst of a real laptop's uploads and session page reads against the current allocator, two arenas and one, and compare the failures and memory.
- [ ] 2.2 Recreate ClickHouse on the laptop install and load the session pages that failed.
