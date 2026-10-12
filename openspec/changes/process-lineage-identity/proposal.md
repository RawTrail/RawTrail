## Why

The server cannot follow a process back to the one that created it once the process is reparented, and it cannot group processes beyond a bare `ppid` (#1277, epic #1275). A double-forked daemon reports `ppid` 1, so the work an AI agent or an attacker detaches from a shell appears unrelated to it. ESF already reports the identity needed to fix this, and the extension drops it. The exec event's `cwd` is always empty.

The spike in #1276 settled which fields matter:

- The fork event's forking process is the child's exact creator. Reparenting cannot change it, so the forking process's `pidversion` is the exact lineage edge.
- On exec, the parent audit token names the current parent, which is launchd for any daemonized process.
- `original_ppid` survives reparenting as a pid only.
- The process group survives double forks, `nohup`, and `disown`; `setsid` replaces it. That makes it a per-tool-call grouping key for agent shells.
- The responsible process attributes XPC services to the app that requested them. It is the terminal app for every process started from a terminal.

## What Changes

- `fork` payloads carry `parent_pidversion`, the forking process's generation.
- `exec` payloads carry `original_ppid`, `parent_pidversion` (of the parent named by `ppid`), `responsible_pid`, `responsible_pidversion`, `session_id`, `group_id`, `tty` (only with a controlling terminal), `start_time_ns`, and a populated `cwd`.
- Startup snapshot `exec` payloads carry `group_id` and `start_time_ns` from the process table.
- Every new field is optional and omitted when unknown, so older agents and the Windows sensor stay valid. The server ignores the fields until #1280 stores them.

## Impact

- `extension/edr/extension/ProcessLineage.swift` (new), `EventSerializer.swift`, `ESFSubscriber.swift`, `ProcessSnapshotEnumerator.swift`, and `schema/events.json`.
- Wire size grows by about 170 bytes per exec event (166 measured on edr-dev without a `tty`).
