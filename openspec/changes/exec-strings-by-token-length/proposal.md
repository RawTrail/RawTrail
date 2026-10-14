## Why

The exec event read its path and each argument with `String(cString:)`, which scans for a NUL. Endpoint Security string tokens carry their length and are not guaranteed to be NUL-terminated, so the read could run past a token into whatever follows it (#1293). Every other token read in the extension already goes through `esTokenString`.

## What changes

- The exec path and arguments are read by their token's length, through `esTokenString`, like the working directory, the terminal and the signing identifiers.
- A path or argument that is not valid UTF-8 is reported empty, as every other token is, rather than with replacement characters.
- The reads of genuinely NUL-terminated C strings (`strerror`, `pw_dir`, `proc_pidpath`) are commented as such.

## Impact

- `extension/edr/extension/ESFSubscriber.swift`, `ESFStringToken.swift`, a new `TokenString.swift` with its tests, and comments in `NotificationClient.swift`, `WatchedPaths.swift` and `SnapshotSigning.swift`.
