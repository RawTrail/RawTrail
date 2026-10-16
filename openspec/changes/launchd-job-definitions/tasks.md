## 1. Agent

- [x] 1.1 Read each plist a `launchctl load|bootstrap` names, for its caller, bounded; parse XML, and binary through plutil on stdin.
- [x] 1.2 Send the definitions as `launchd_jobs` on the exec.

## 2. Server

- [x] 2.1 Store `launchd_jobs` on the row (migration 00022).
- [x] 2.2 Link a load or bootstrap to each job it defined, by label, held to the session it loads into.

## 3. UI

- [x] 3.1 Process detail lists the jobs a launchctl exec loaded.

## 4. Tests and QA

- [x] 4.1 Agent unit, property and fuzz tests; graph unit and integration tests; vitest; mutation-checked.
- [x] 4.2 edr-dev: a Claude Code session loads and bootstraps two jobs; the new agent sends their definitions, and lane A links them.
