## 1. Server

- [x] 1.1 One archive read of the tokens of a page of sessions, agreeing with each session's usage totals
- [x] 1.2 Each list row's `tokens`, left out when the archive cannot be read

## 2. UI

- [x] 2.1 A Tokens column on the Agent sessions page

## 3. Tests

- [x] 3.1 ClickHouse PBT: the batched read equals each session's totals, with copies, forks, re-sent messages and two hosts
- [x] 3.2 Integration: the list's totals and an unreadable archive; vitest; the Playwright journey
