## ADDED Requirements

### Requirement: Launchd job definitions on the row

The graph builder SHALL store an exec's `launchd_jobs` on the process row it writes for the exec, as the JSON the exec sent, and SHALL leave it unset for any other exec.

#### Scenario: An exec stores the jobs it loaded

- **GIVEN** a `launchctl load` exec carrying two job definitions, and the shell that ran it carrying none
- **WHEN** the graph builder processes the events
- **THEN** the launchctl row holds both definitions with their labels and program arguments, and the shell's row holds none
