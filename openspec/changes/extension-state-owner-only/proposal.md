## Why

The extensions persist the documents pushed to them under `/var/db/com.fleetdm.edr/`: the application-control snapshot, the watched-path set, the network containment state and the record of disabled providers. `AtomicFile.write` set no mode, so the umask decided. Measured on a macOS 26 VM, the directory was 0755 and every file 0644 (#1229): any local user could read what the EDR watches and blocks and which addresses a contained host can still reach, which is a map for evading it.

## What Changes

- The state directory is owner-only (0700) and each persisted file is owner-only (0600). Only root reads them: both extensions and the agent run as root.
- A directory left open by an earlier install is tightened by the next write. That step is best effort, because a failed write would lose the document across a restart while a listable directory exposes only file names. The file mode is not best effort.

## Impact

- `extension/edr/shared/AtomicFile.swift`, used by every extension store.
- Existing files written before this change keep their mode until rewritten, but the owner-only directory already denies other users access to them.
