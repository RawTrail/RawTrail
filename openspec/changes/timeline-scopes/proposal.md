## Why

The host timeline's alert-chain scope is a list of `pid:pidversion` pairs the browser assembles from the process-tree page it loaded (#1282). The chain is only as complete as that page, the list can run to thousands of pairs in a query string, and the client carries the logic for deciding how much of the chain the list covers. The server already reads chains, stories and sessions by their indexes.

## What changes

- `GET /api/hosts/{id}/timeline` accepts `scope=chain&pin=<process id>`, `scope=session&session=<id>` and `scope=story&root=<process id>`. The server reads the scope with the process tree's own reads (the chain read, or the indexed story or session read) and filters the events to the generations it finds, at most 5,000.
- The response carries a `scope` report: whether the scope applied, how many processes it has, how many of them carry no generation (their events cannot be selected), and whether it was truncated. A scope with no generation to match returns the whole host's stream with `applied` false, so the client can say why.
- A scope without a usable target, or one combined with `chain=`, is rejected with `invalid_scope`. `chain=` keeps working; the UI moves to `scope=` in the next PR.

## Impact

- `server/detection/internal/operator/host_timeline_{handler,scope}.go`, the API doc and the OpenAPI spec.
