## Why

`launchctl load <plist>` and `launchctl bootstrap <domain> <plist>` name a file, and the job they start is defined inside it: its label, and the program it runs. Without the definition, the process tree cannot link the command to the job launchd starts, and a persistence detection cannot see what a newly loaded LaunchAgent or LaunchDaemon runs (MITRE T1543.001). Measured on edr-dev (#1341): launchd's xpcproxy row for each job names its label, the same instant the load runs.

## What changes

- **Agent:** on a `launchctl load|bootstrap` exec, the agent sends each named plist's `Label`, `Program` and `ProgramArguments`, capped, as the exec's `launchd_jobs`, at most eight plists.
  - **As the user:** the plist is read by `head` running as the user who ran launchctl, with no supplementary groups, so the kernel decides whether that user could read it.
  - **Bounded:** at most 64 KiB plus one byte is read. XML is parsed directly; a binary plist's bytes go through `plutil` on stdin.
  - **Off the critical path:** the event loop waits at most 300 ms for a read, one at a time, so a stalled read cannot hold back other telemetry.
- **Graph builder:** stores `launchd_jobs` on the exec's row, in a new nullable column.
- **Tree reads:** link the load or bootstrap to the program launchd started from each xpcproxy row naming one of those labels, within 15 seconds, one launch per job. A bootstrap is held to the user its domain names, a load to its caller unless root.
- **UI:** process detail lists each job a launchctl exec loaded, with what it runs and the plist it came from.

## Impact

- `agent/enrich/` and the agent's event loop, `schema/events.json`, a migration adding `processes.launchd_jobs`, the store's write paths, `server/detection/internal/graph/launch.go`, process detail, OpenAPI and the CHANGELOG. Needs a VM run before RC.
