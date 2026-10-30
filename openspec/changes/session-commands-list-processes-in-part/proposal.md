## Why

A page of a session's commands listed every process of every command on it, so one command that started thousands of processes (a build, a test run with a worker per case) made the page as large as all of them, whatever its limit. Issue #1443.

## What changes

- Past 100 processes, a command on a page lists its first 100, every alerted process and every process the page's search finds, each under its creators, and says it was cut. Its process ids stay whole, so its size and alerts are still counted.
- `?command=<key>` reads one command with every process listed.
- The session view offers "Show all N processes" on a cut command and lists it whole.

## Impact

- `server/detection/internal/sessioncommands`, the commands endpoint and a read of a session's alerted processes, `docs/api/openapi.yaml`, `ui/src/components/Sessions/SessionView.tsx`.
