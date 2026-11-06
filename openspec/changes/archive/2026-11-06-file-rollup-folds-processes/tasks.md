## 1. Extension

- [x] 1.1 Keep each observed generation's parent in `AgentProcessSet`, an exec keeping its predecessor's
- [x] 1.2 Record each process's lineage in the rollup when its first operation arrives
- [x] 1.3 Fold rows by process, then by directory, in turn until the window fits, settling each on the nearest process its rows share
- [x] 1.4 Send `process_count` on a row that folds several processes

## 2. Server and UI

- [x] 2.1 List a folded row apart from the named process's own, with `processes`, and carry the distinction in the page cursor
- [x] 2.2 Show the count in the Files tab and the host timeline

## 3. Tests

- [x] 3.1 An 80-job fan-out folds into one row on the process that started it, with nothing dropped
- [x] 3.2 Two sessions' jobs on one file fold into two rows, one per session
- [x] 3.3 Property: every process a row counts descends from the one it names, which is the nearest they share, and no row spans two subtrees
- [x] 3.4 Exercise on edr-dev with the same 80-job pipeline
