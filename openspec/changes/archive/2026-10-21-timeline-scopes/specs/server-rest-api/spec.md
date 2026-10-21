## ADDED Requirements

### Requirement: Host timeline scopes

The host timeline SHALL accept `scope=chain&pin=<process id>`, `scope=session&session=<id>` and `scope=story&root=<process id>`, read the scope's processes with the process tree's reads, filter events to their generations, and report whether it applied, its process count, how many carry no generation, and truncation. A scope with no generation SHALL return the whole host's stream, reported not applied. A scope without a target, or with `chain=`, SHALL be rejected.

#### Scenario: The alert chain is resolved by the server

- **GIVEN** an alerted process with ancestors and descendants, and another process that reused one of their pids
- **WHEN** the timeline is read with `scope=chain` pinning the alerted process
- **THEN** only events of the chain's generations are returned
- **AND** the report counts the chain's processes and those without a generation

#### Scenario: A session or story scopes the timeline

- **GIVEN** an agent session's processes among the host's other activity
- **WHEN** the timeline is read with `scope=session`, or `scope=story` naming the story's root
- **THEN** only events of that scope's generations are returned

#### Scenario: A scope with nothing to match shows the whole host

- **GIVEN** a scope naming no stored process, or one whose processes carry no generation
- **WHEN** the timeline is read with it
- **THEN** the whole host's stream is returned with the scope reported not applied and its process count

#### Scenario: A malformed timeline scope is rejected

- **GIVEN** a scope without a usable pin, session or root, or one combined with `chain=`
- **WHEN** the timeline is read
- **THEN** the response is a client error with `invalid_scope`
