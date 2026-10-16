## ADDED Requirements

### Requirement: Launchd job definitions

On a `launchctl load` or `launchctl bootstrap` exec, the agent SHALL read each plist the exec names, at most eight, resolving a relative path against the exec's working directory, and SHALL send each one's `Label`, `Program` and `ProgramArguments`, capped, as `launchd_jobs`. It SHALL read a plist only when the user who ran launchctl could read it, and SHALL read at most 64 KiB of it.

#### Scenario: A launchctl load carries the jobs it names

- **GIVEN** `launchctl load -w -S Aqua /tmp/a.plist b.plist unreadable.plist` run in `/Users/v/proj`
- **WHEN** the agent handles the exec
- **THEN** it carries the definitions of `/tmp/a.plist` and `/Users/v/proj/b.plist`, and none for the plist it could not read

#### Scenario: A plist the user could not read is not read for them

- **GIVEN** a plist mode 0600 owned by one user
- **WHEN** another user runs `launchctl load` on it, directly or through a symlink
- **THEN** the agent does not read it, though it reads a world-readable plist for anyone and a user's own plist for its owner
