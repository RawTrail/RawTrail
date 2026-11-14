## Why

On the v0.9.0-rc.9 laptop install, a session page showed "The session's summary could not be read." in the minute after an upgrade, while the agent uploaded the events it had queued during the reinstall. ClickHouse refused the page's reads and some of the uploads at its 953 MiB cap, though it had about 200 MiB allocated. Its allocator (jemalloc, built with 64 KB pages) kept an arena for each of the Mac's 16 cores and held twice the memory it had allocated, and ClickHouse counts that resident memory against its cap. Each of the page's reads uses 20 to 40 MB, so the headroom went to fragmentation, not to the reads.

## What changes

- The laptop ClickHouse runs its allocator with one arena (`MALLOC_CONF=percpu_arena:disabled,narenas:1`). The environment is part of the compose service, so the next `rawtrail-laptop install` recreates the container with it.

## Impact

- `docker-compose.laptop.yml`, `test/arch/laptop_test.go`.
- Only the laptop profile. A server deployment's ClickHouse is unchanged.
