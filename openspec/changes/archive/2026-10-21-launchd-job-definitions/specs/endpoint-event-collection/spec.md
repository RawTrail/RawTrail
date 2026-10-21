## ADDED Requirements

### Requirement: Launchd job definitions

On a `launchctl load` or `bootstrap` exec, the agent SHALL send the capped `Label`, `Program` and `ProgramArguments` of each plist it names, or of the `.plist` files directly in a directory it names, at most eight, as `launchd_jobs`. It SHALL read a plist as the user who ran launchctl, without supplementary groups, at most 64 KiB of it, and SHALL hold its event loop at most a bounded time for the read, sending the exec without what it could not read in time.

#### Scenario: A launchctl load carries the jobs it names

- **GIVEN** `launchctl load -w -S Aqua /tmp/a.plist b.plist unreadable.plist` run in `/Users/v/proj`
- **WHEN** the agent handles the exec
- **THEN** it carries the definitions of `/tmp/a.plist` and `/Users/v/proj/b.plist`, and none for the plist it could not read

#### Scenario: A plist the user could not read is not read for them

- **GIVEN** a plist mode 0600 owned by one user
- **WHEN** another user runs `launchctl load` on it, directly or through a symlink
- **THEN** the agent does not read it, though it reads a world-readable plist for anyone and a user's own plist for its owner

#### Scenario: A blocked plist read does not hold back telemetry

- **GIVEN** a plist read that does not finish, as on a stalled mount or a named pipe held open
- **WHEN** the agent handles the exec, and another launchctl exec while that read still runs
- **THEN** the first waits no longer than the bound and the second not at all, both go out without the definitions, and reads run again once the stuck one ends

#### Scenario: What is no plist yields no job

- **GIVEN** a `launchctl load` naming a file over 64 KiB, a missing path, a directory the user cannot list, or an exec whose uid or gid is outside a uid's 32-bit range
- **WHEN** the agent handles the exec
- **THEN** it reads no job for that operand, and the exec goes out without one for it

#### Scenario: A directory operand carries the jobs of the plists in it

- **GIVEN** `launchctl load agents` run in a directory whose `agents` holds `a.plist`, `b.plist`, `notes.txt` and a `nested/` subdirectory with a plist
- **WHEN** the agent handles the exec
- **THEN** it carries the jobs of `a.plist` and `b.plist`, listed and read as the user, as launchctl itself loads, and none from `notes.txt` or `nested/`
