## 1. Server

- [x] 1.1 Activity rollup reads `file_activity`: payload operation, weighted counts, rolled-up rows.
- [x] 1.2 `file_activity` joins the timeline's event classes.

## 2. UI

- [x] 2.1 The Files tab marks rolled-up rows.
- [x] 2.2 The timeline's Files chip and file activity detail.

## 3. Tests

- [x] 3.1 ClickHouse integration test for agent file rows; timeline handler test.
- [x] 3.2 vitest for the Files tab and the timeline.
- [x] 3.3 Playwright: the Claude Code session's activity lists git's index update.
