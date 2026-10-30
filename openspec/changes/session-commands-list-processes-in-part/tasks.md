## 1. Server

- [x] 1.1 Cap each command's listed processes, keeping its alerted processes and the search's hits under their creators
- [x] 1.2 Read one command whole by its key

## 2. UI

- [x] 2.1 "Show all N processes" on a cut command

## 3. Tests

- [x] 3.1 PBT: the cut lists exactly the first processes, the kept ones and their creators, in order and under their parents
- [x] 3.2 Handler: a large command cut, a hit and an alert past the cap listed, one command read whole, an unknown command
- [x] 3.3 vitest: a cut command read whole, and a failed read
