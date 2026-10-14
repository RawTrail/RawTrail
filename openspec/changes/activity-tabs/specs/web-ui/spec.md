## ADDED Requirements

### Requirement: Activity tabs

Process detail and the session view SHALL show what their scope touched in Files, Network and DNS tabs, each with its totals and a list naming the process behind each entry. Process detail SHALL switch between the process alone and its whole subtree. The tabs SHALL say when a list was cut and when the scope reached only part of its processes, or none.

#### Scenario: A session's network tab lists every destination with its process

- **GIVEN** an agent session whose processes connected to destinations
- **WHEN** the operator opens the session's activity
- **THEN** the Network tab states the connection and destination totals and lists every destination with the process that made each connection

#### Scenario: A process's activity switches between it and its subtree

- **GIVEN** a process's detail
- **WHEN** the operator opens its activity, then switches to the whole subtree
- **THEN** the activity is first read for the process alone, then for it and its descendants

#### Scenario: The tabs say how much of the scope they reflect

- **GIVEN** an activity read whose scope matched no stored process, matched processes none of which carry a generation, was truncated, or left some processes unmatched
- **WHEN** the tabs render
- **THEN** each case is stated in its own words, and a scope read in full carries no such statement
