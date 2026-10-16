## ADDED Requirements

### Requirement: Loaded launchd jobs

Process detail SHALL list each job a launchctl exec loaded: its label, what it runs, and on hover the plist it came from, and SHALL say when the agent cut a definition to its bounds.

#### Scenario: Process detail names the jobs a launchctl loaded

- **GIVEN** a `launchctl load` whose row carries three jobs: one with program arguments, one with a program and cut, one with only a label
- **WHEN** its process detail opens
- **THEN** each is listed as a loaded job by label, the first two with what they run and the cut one marked, and the third with no program
