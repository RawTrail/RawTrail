## ADDED Requirements

### Requirement: Agent session digest

The agent session summary SHALL carry `digest`, over the scope its activity totals read: the session's prompt count, the programs its commands ran most, its busiest directories, the files it changed with those outside its project (Claude Code's own files and temporary files counted apart), its busiest domains each marked when new to the host, and its events and commands in equal buckets of its span.

#### Scenario: The digest describes what the session touched

- **GIVEN** a session that was sent prompts, wrote files in and outside its project, and looked up and connected to domains, one of which the host had looked up before
- **WHEN** a client reads its summary
- **THEN** `digest` counts its prompts, groups its file events by directory, and groups its lookups and named connections by registrable domain under the public suffix list, marking every domain but the one seen before as new

#### Scenario: The digest counts the files the session changed

- **GIVEN** a session that changed files in and outside its project, made and removed a scratch file, wrote a file by renaming a temporary one over it, read a file, reached a socket, and wrote Claude Code's own transcript and settings
- **WHEN** a client reads its summary
- **THEN** `digest.files.changed` counts the files it changed, the renamed-over target among them, and `digest.files.outside_project` those outside its project
- **AND** the scratch file and the renamed temporary file are counted in `digest.files.temporary`, Claude Code's own files in `digest.files.agent_files`, and the read and the socket nowhere

#### Scenario: The digest names the programs the commands ran

- **GIVEN** a session whose commands ran programs, some through the agent's wrapper shell
- **WHEN** a client reads its summary
- **THEN** `digest.programs` counts the processes by program, most run first, the wrapper shell left out

#### Scenario: The digest carries a timeline of the session

- **GIVEN** a session whose first command forked before its first sighting, and whose events continued after its last
- **WHEN** a client reads its summary
- **THEN** `digest.timeline` and `digest.command_timeline` count every event and command in equal buckets of a span widened to hold them all, from `timeline_start_ns`, `timeline_bucket_ns` wide
