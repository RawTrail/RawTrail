## MODIFIED Requirements

### Requirement: Activity table cells stay on one line

Each cell of an activity table (Files, Network, DNS) SHALL keep its value on one line, except a DNS row's answers, which SHALL wrap within a capped width. A file row's cell SHALL be capped in width: its path, or a rename's two, SHALL be cut at its start, so its file name stays in view, with the whole path as its tooltip, and a tag beside it, such as a socket's daemon, SHALL keep its width. When the table is still wider than its frame, the frame SHALL scroll sideways rather than wrap the value.

#### Scenario: A file path in the process detail panel reads whole

- **GIVEN** a process whose file activity names a path longer than the panel's path column would otherwise allow
- **WHEN** the operator opens its Files activity in the process detail panel at an 800 pixel wide viewport
- **THEN** the path's cell is one line tall

#### Scenario: A long path is cut at its start and shown whole on hover

- **GIVEN** a file row whose path is longer than the cap, a rename between two such paths, and a connect to a well-known daemon's socket
- **WHEN** the Files tab lists them
- **THEN** each cell holds its paths, the rename's arrow and the daemon's tag in one capped row, and each path carries its whole path as its tooltip
- **AND** a copy of the cell keeps the whole paths with a space between its parts

#### Scenario: A long answer list wraps within its cap

- **GIVEN** a DNS name answered by more addresses than fit the answers cap on one line
- **WHEN** the DNS tab lists it
- **THEN** its answers wrap onto further lines within the cap, and the table does not widen past it

## ADDED Requirements

### Requirement: Activity times read as an age

The Files, Network and DNS lists SHALL show when each row was first and last seen as an age relative to now, such as "6m ago", the way the Agent sessions list shows a time, with the exact time as each one's tooltip. A row seen once SHALL show no last.

#### Scenario: First and Last read as an age with the exact time on hover

- **GIVEN** a DNS name first resolved six minutes ago and a connection made once
- **WHEN** the operator opens the scope's activity
- **THEN** the DNS row's First reads "6m ago" with its exact time as the tooltip
- **AND** the connection's Last is empty

### Requirement: The DNS list sorts by name

The DNS list's Name header SHALL order the rows listed by name when selected, and back to when each was first seen when selected again, and SHALL state the order it applies. The order SHALL hold while the operator visits another class and returns.

#### Scenario: The Name header sorts the DNS list

- **GIVEN** a DNS list whose names were first resolved out of alphabetical order
- **WHEN** the operator selects the Name header
- **THEN** the rows are listed by name and the header states it sorts ascending
- **AND** selecting it again lists them by when each was first seen
