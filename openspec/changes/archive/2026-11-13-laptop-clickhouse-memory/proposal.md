## Why

On a real Mac running the v0.9.0-rc.8 laptop install, ClickHouse reached its 953 MiB cap within about two hours of use and refused the agent's event uploads (HTTP 500) and its own background merges, while its table held 53 MB. The laptop config shrank the caches but left ClickHouse's server-sized defaults: 64 merge threads and other pools sized for many cores, trace-level logging (about 250,000 lines an hour, each also written to `system.text_log`), and the query log. About one insert a second drove tens of thousands of merges, and the memory each pool thread used stayed with the process.

## What changes

- The laptop ClickHouse config sizes the background pools for one Mac's inserts, reads smaller blocks in merges, logs at warning level, and drops the text and query logs.
- The installer labels the ClickHouse service with a digest of its config, so `rawtrail-laptop install` recreates the container when the config changed. Without it compose sees no change in a mounted file, and an existing install kept its old settings.

## Impact

- `packaging/laptop/clickhouse-laptop.xml`, `docker-compose.laptop.yml`, `scripts/laptop/rawtrail-laptop`, `test/arch/laptop_test.go`.
- An existing laptop install takes the new config the next time `rawtrail-laptop install` runs from a checkout that has it.
