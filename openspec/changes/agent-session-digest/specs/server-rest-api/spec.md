## ADDED Requirements

### Requirement: Agent session digest

The agent session summary SHALL carry `digest`, over the scope its activity totals read: the session's prompt count, the programs its commands ran most, its busiest directories with the paths outside its project, its busiest domains each marked when new to the host, and its events and commands in equal buckets of its span.

#### Scenario: The digest describes what the session touched

- **GIVEN** a session that was sent prompts, wrote files in and outside its project, and looked up and connected to domains, one of which the host had looked up before
- **WHEN** a client reads its summary
- **THEN** `digest` counts its prompts, groups its file events by directory with the paths outside its project counted, and groups its lookups and named connections by registrable domain, marking every domain but the one seen before as new

#### Scenario: The digest names the programs the commands ran

- **GIVEN** a session whose commands ran programs, some through the agent's wrapper shell
- **WHEN** a client reads its summary
- **THEN** `digest.programs` counts the processes by program, most run first, the wrapper shell left out

#### Scenario: The digest carries a timeline of the session

- **GIVEN** a session with events and commands across its span
- **WHEN** a client reads its summary
- **THEN** `digest.timeline` and `digest.command_timeline` count them in equal buckets of the span, the span's last moment in the last bucket, and nothing outside it
