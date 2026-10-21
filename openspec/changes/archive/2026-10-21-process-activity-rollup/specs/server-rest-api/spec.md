## ADDED Requirements

### Requirement: Process activity rollup

`GET /api/hosts/{host_id}/activity` SHALL total and list the files, network destinations and DNS names that a process, its subtree, an AI agent session or a story touched, naming the process behind each entry. It SHALL match events to processes by exact generation, count a coalesced event as the occurrences it stands for, cap each list and report the cut, and report how far the scope reached. It SHALL require process read.

#### Scenario: A scope's activity is totalled and listed by process

- **GIVEN** two processes that opened, renamed, connected and resolved, including a coalesced connection standing for three, an event of a later process that reused one's pid, and an event with no generation
- **WHEN** their activity is read
- **THEN** each class's event total and distinct count match a count by hand, the coalesced connection counting three
- **AND** each list names the process behind each entry, and leaves out the reused pid's event and the event with no generation
- **AND** a list read with a smaller cap reports that it was cut, while the totals stay the scope's

#### Scenario: A process or its subtree is totalled

- **GIVEN** a process with an ancestor and two children, one of which carries no generation
- **WHEN** the activity is read for the process alone, and then for its subtree
- **THEN** the first reads only the process, and the second the process and its child with a generation, never the ancestor
- **AND** the scope report counts the child without a generation as unmatched

#### Scenario: A session or story is totalled

- **GIVEN** an AI agent session, or a story, read by its index
- **WHEN** its activity is read
- **THEN** every process of it that carries a generation is matched, and the scope report says when the read was truncated

#### Scenario: A malformed activity scope is rejected

- **GIVEN** a request with no scope, a scope without its target, or the timeline's `chain` scope
- **WHEN** it is made
- **THEN** it is rejected with `invalid_scope`
