# Report the Unix-domain sockets an AI agent session connects to

## Why

When an agent asks a running daemon for work over a Unix-domain socket (`docker run` to `/var/run/docker.sock`, `ssh-add` to an ssh-agent, `psql` to a local Postgres), the session showed the client process and nothing to say the work was handed off (issue #1355, from the #1342 spike).

## What changes

- The agent-scoped file subscriber also subscribes to Endpoint Security's Unix-domain socket connect event, and reports each connect by an agent-session process as `file_activity` with operation `connect` and the socket's path.
- The request is attributed, never the daemon's later work: a shared daemon serves many sessions, and the connect is the session's own, kernel-attributed process.

## Impact

- Wire: `connect` joins the `file_activity` operations.
- UI: the Activity view's Files list shows connects as the operation `connect`.
- Naming the daemon behind a path is the follow-up PR of #1355.
