## 1. Defaults

- [x] 1.1 Quickstart: `--innodb-buffer-pool-size=${EDR_MYSQL_BUFFER_POOL_SIZE:-1G}`
- [x] 1.2 Laptop: 512 MB buffer pool under a 1,024 MB container limit
- [x] 1.3 Document the setting in `docs/quickstart-vm.md`

## 2. Verification

- [x] 2.1 Tests read both compose files and pin the defaults and the override
- [ ] 2.2 Dogfood runs the rc with `EDR_MYSQL_BUFFER_POOL_SIZE=3G` and MySQL reports a 3 GiB buffer pool
