## Why

The timeline scoped to the alert chain by assembling `pid:pidversion` pairs from the process tree the graph had loaded (#1282). The scope was only as complete as that page, and the timeline's honesty about it rested on the state of a different read: whether the tree was still loading, had failed, or had truncated. The server now resolves the chain itself and reports how far the scope reached (`timeline-scopes`).

## What changes

- With the alert-chain focus on, the timeline passes the alerted process (`scope=chain&pin=`) and states the server's report:
  - fully scoped;
  - partly scoped, because some processes carry no generation;
  - partly scoped, because the chain is larger than the timeline can scope to;
  - showing the whole host, because the process is no longer stored;
  - showing the whole host, because the chain carries no generation.
- The client no longer assembles generations, and the timeline no longer depends on the graph's tree read, so the in-flight and failed-tree-read cases no longer apply.
- The "Host event timeline view" requirement is removed and restated as "Host timeline view" under 500 characters, with its detail in the scenarios. It is a removal rather than a modification because two of its scenarios, the in-flight and failed-tree-read cases, no longer apply.

## Impact

- `ui/src/{api,types}.ts`, `ui/src/components/{HostTimeline,ProcessTree,chainScope,ProcessTree.helpers}.ts(x)` and their tests.
