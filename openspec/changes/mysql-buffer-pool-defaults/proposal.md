## Why

MySQL's own 128 MB buffer pool is far smaller than the process graph a busy host builds, so an agent session's views read most of their pages from disk. On dogfood (v0.9.0-rc.5, an 11.4 GB processes table) the session tabs took 1 to 3 s per MySQL read, against 0.05 to 0.3 s for the same reads served from memory (issue #1550). The quickstart left the buffer pool at that default, and the laptop stack pinned it there.

## What changes

- The quickstart stack gives MySQL a 1 GB buffer pool, resized with `EDR_MYSQL_BUFFER_POOL_SIZE` in `.env`.
- The laptop stack gives MySQL a 512 MB buffer pool and raises its container limit from 640 MB to 1,024 MB to keep room above it.
- `docs/quickstart-vm.md` documents the setting.

## Impact

- `docker-compose.quickstart.yml`, `docker-compose.laptop.yml`, `docs/quickstart-vm.md`, `docs/watch-your-agent.md`.
- An existing install picks up the new size the next time MySQL is recreated by `docker compose up -d`. The laptop stack's memory caps rise by about 400 MB.
