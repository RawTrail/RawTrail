## Why

An app an agent opens is linked to its session (an inferred launch, #1286), but the XPC services the app asks for are not. launchd starts them, and they carry none of the session's markers. Each service's exec does record the app, by pid and generation, as its responsible process. Measured on edr-dev with `open -a TextEdit page.html`: seven services named TextEdit, which the session held (#1340).

## What changes

- **Session and story reads:** a process launchd started that names a process in the read as responsible, matched by pid and generation within that process's lifetime, is linked to it with a new `responsible_process` link and added with its descendants as an extra root. A window read links only what it holds. Reads cap the services per app and report the cut.
- **The UI:** a service is listed under the app it serves, badged as a service, in Commands, Processes and the host graph's logical tree.
- **Out of scope:** command-line tools. A process started from a shell names the terminal or SSH login as responsible (measured with `textutil`), so its services cannot be linked this way.

## Impact

- `server/detection/internal/graph/service.go` and `scoped.go`/`query.go`, `server/detection/internal/mysql/launch.go`, `server/detection/api/types.go`, the OpenAPI spec, the UI's link handling and session rows, and tests.
