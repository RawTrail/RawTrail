## Why

Work an AI agent hands to launchd appears as an unrelated root (#1286). The #1276 spike found `open -a` and `launchctl` jobs fully disconnected from their caller: parent 1, their own process group, responsible for themselves. On edr-dev, a Claude Code 2.1.296 session on macOS 26.3 ran `open -a TextEdit` and `launchctl submit -- /bin/sleep 61`. TextEdit inherited the agent's environment, so its session markers already attribute it to the session, but nothing ties it to the `open` that started it. The `launchctl` job carries no markers and is not attributed at all.

## What changes

- Tree reads infer launches: for each `open -a <App>`, `open <path>.app` or `launchctl submit ... -- <program>` exec in the read, the first process launchd started within 5 seconds of it whose executable runs that app or program is linked, oldest launcher first and each launched process at most once.
- The tree response gains `links` (`inferred_launch`). A session or story read adds a linked process it does not hold, with its descendants and without launchd's `xpcproxy` row, as an extra root. A window read links only what it holds.
- A link is a correlation, never lineage: the launched process keeps its own parentage, session and tool call. `open -b`, `launchctl load|bootstrap` and XPC hops name nothing to match and are not inferred; macOS 27's `AUTH_XPC_CONNECT` and `BOOTSTRAP_LOOK_UP` events are the follow-up for exact links.

## Impact

- `server/detection/{api,internal/graph,internal/mysql}`, `docs/api/openapi.yaml`, `docs/api.md`.
