## Why

MySQL's own 128 MB buffer pool is far smaller than the process graph a busy host builds, so reads that walk it come from disk. On dogfood (v0.9.0-rc.5, an 11.4 GB processes table) MySQL had done about 247 million disk reads since it started, and a long agent session's process tree took about 2.7 s to load. With a 3 GiB pool the same tree took 1.7 s, and the session's tree, conversation, commands and summary reads together caused about 950 disk reads, with no statement over 20 ms (issue #1550). The quickstart left the buffer pool at MySQL's default, and the laptop stack pinned it there.

## What changes

- The quickstart stack gives MySQL a 1 GB buffer pool, resized with `EDR_MYSQL_BUFFER_POOL_SIZE` in `.env`.
- The laptop stack gives MySQL a 512 MB buffer pool and raises its container limit from 640 MB to 1,024 MB to keep room above it.
- `docs/quickstart-vm.md` documents the setting.

## Impact

- `docker-compose.quickstart.yml`, `docker-compose.laptop.yml`, `docs/quickstart-vm.md`, `docs/watch-your-agent.md`.
- An existing install picks up the new size the next time MySQL is recreated by `docker compose up -d`. The laptop stack's memory caps rise by about 400 MB.
