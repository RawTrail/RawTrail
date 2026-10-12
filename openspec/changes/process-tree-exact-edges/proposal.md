## Why

The graph builder now records each process's creator row (#1280, epic #1275), but the process forest still hangs a child under the newest row holding its `ppid`. Under pid reuse that is a different process, and the ancestor walk for a pinned process resolves each parent by pid and time, so it can climb into the process that reused the number. Analysts tracing what an AI agent did lose the chain at exactly those points.

## What changes

- The forest links a process to its creator row. When that row is not in the result, the process is a root rather than being matched by number.
- A process without a creator row links to the row with its exact `(ppid, parent_pidversion)` in the result, and only then to the newest row holding its `ppid`.
- The ancestor walk for a pinned process reads the creator row by id. A pruned creator ends the chain.
- A chain's descendants exclude a child whose creator row is another process.
- The tree, child, process-by-id and process-by-generation reads return the lineage fields, and the UI's process type carries them.

## Impact

- `server/detection/internal/graph/{query,chain}.go`, `server/detection/internal/mysql/{processes,processbatch}.go`, and `ui/src/types.ts`.
- API responses gain optional lineage fields; existing clients ignore them.
