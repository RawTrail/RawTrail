## Why

The extensions persist the documents pushed to them under `/var/db/io.rawtrail/`: the application-control snapshot, the watched-path set, the network containment state and the record of disabled providers. `AtomicFile.write` set no mode, so the umask decided. Measured on a macOS 26 VM, the directory was 0755 and every file 0644 (#1229): any local user could read what the EDR watches and blocks and which addresses a contained host can still reach, which is a map for evading it.

## What Changes

- Each document is written to a temporary file created owner-only (0600), flushed, and renamed over the destination, so it is never readable by another user, even for a moment. The state directory is owner-only (0700). Only root reads them: both extensions and the agent run as root.
- A directory left open by an earlier install is tightened by the next write, so older files beside it stop being readable. That step is best effort, because failing the write over it would lose the document across a restart, and the document itself is already protected.

## Impact

- `extension/edr/shared/AtomicFile.swift`, used by every extension store.
- Existing files written before this change keep their mode until rewritten, but the owner-only directory already denies other users access to them.
