## ADDED Requirements

### Requirement: Story and session tree reads

The process-forest endpoint SHALL accept `scope=story&root=<process id>` and `scope=session&session=<id>`, returning the host's processes of that story or AI agent session by an indexed read with no time window, oldest first, bounded by the row limit with the window read's truncation and count metadata. A story or session scope without a valid root or session SHALL be rejected as a client error.

#### Scenario: A session is read whole

- **GIVEN** an agent session whose processes outnumber the chain read's descendant cap
- **WHEN** the forest is read with `scope=session` and a limit above the session's size
- **THEN** every process of the session is returned, linked under its creator, and the read is not truncated
- **AND** the read costs one query whatever the depth of the tree

#### Scenario: A scope beyond the limit reports truncation

- **GIVEN** a story with more processes than the row limit
- **WHEN** the forest is read with `scope=story`
- **THEN** the oldest processes up to the limit are returned
- **AND** the read is reported as truncated with the story's count

#### Scenario: A scope reads only its host

- **GIVEN** the same session id or story root id on two hosts
- **WHEN** one host's forest is read with that scope
- **THEN** only that host's processes are returned

#### Scenario: A scope without its target is rejected

- **GIVEN** `scope=story` without a positive `root`, or `scope=session` without a `session` of at most 256 bytes
- **WHEN** the forest is read
- **THEN** the response is a client error with `invalid_scope`
