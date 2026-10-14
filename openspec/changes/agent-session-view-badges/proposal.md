## Why

The session view (`agent-session-view`) shows what each tool call ran, but not what was flagged or what changed privilege, and its header is read from the processes the tree returned, so it cannot say when retention has removed part of the session. An operator triaging an agent session needs to see where its alerts were raised and where it became root, and a long session needs to open on what was flagged rather than on every call expanded.

## What changes

- `GET /api/alerts` takes `agent_session=<id>` with `host_id`, selecting the alerts raised on that session's processes; without `host_id` it is rejected, since a session id names a session only on its host. `GET /api/agent-sessions` takes `session=<id>`, so the view can read one session's record.
- The session view's header reads the session's record (agent, version, pid, project, first seen), falling back to the processes when the record cannot be read. Its end time is the later of the record's last seen (the last marked exec) and the processes' last observed activity, since a child can exit after the last exec. It says when the session began before the retention cutoff, so processes that finished before then have been removed.
- Alerts are badged on the process they were raised on, linking to the alert and crediting the rule's author, and on their tool call with the count and the highest severity. The view reads up to 1,000 alerts and says when a session has more. A process an alert was raised on is never hidden as shell plumbing.
- A process that runs as a different user from the process that created it is badged ("runs as root").
- A session of more than 10 tool calls opens with only the calls an alert was raised in expanded; a shorter one opens with every call expanded.
- A failed alert read is reported without hiding the processes.

## Impact

- `server/detection/{api,internal/mysql,internal/operator}`, `docs/api/openapi.yaml`, `docs/api.md`, and their tests.
- `ui/src/api.ts`, `ui/src/components/Sessions/{SessionView,sessionTree}`, and their tests.
