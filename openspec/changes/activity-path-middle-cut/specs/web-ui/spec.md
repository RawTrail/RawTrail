## REMOVED Requirements

### Requirement: Activity table cells stay on one line

**Reason**: A file path is now cut in its middle rather than at its start, and its cell takes the free width rather than a fixed cap. The replacement requirement "Activity cells keep one line and cut paths in the middle" carries the contract.

**Migration**: None for operators. The tests' spec markers are repointed to the replacement's scenarios in the same change.

## ADDED Requirements

### Requirement: Activity cells keep one line and cut paths in the middle

Each cell of an activity table (Files, Network, DNS) SHALL keep its value on one line, except a DNS row's answers and the processes that resolved its name, which SHALL each wrap within a capped width. A file row's path column SHALL take the width the other columns leave, each path cut in its middle to keep its start and its last two segments in view, with the whole path as its tooltip, and a tag beside it SHALL keep its width. A table wider than its frame SHALL scroll sideways rather than wrap.

#### Scenario: A file path in the process detail panel reads whole

- **GIVEN** a process whose file activity names a path longer than the panel's path column would otherwise allow
- **WHEN** the operator opens its Files activity in the process detail panel at an 800 pixel wide viewport
- **THEN** the path's cell is one line tall

#### Scenario: A long path is cut in its middle and shown whole on hover

- **GIVEN** a file row whose path is longer than its cell, a rename between two such paths, and a connect to a well-known daemon's socket
- **WHEN** the Files tab lists them
- **THEN** each cell holds its paths, the rename's arrow and the daemon's tag in one row, each path showing its start and its parent directory and name with the cut between them, and carrying its whole path as its tooltip
- **AND** a copy of the cell keeps the whole paths with a space between its parts

#### Scenario: The path column takes the free width

- **GIVEN** a session's Files tab on a 1920 pixel wide window, listing a path wider than its table
- **WHEN** it is shown
- **THEN** the path fills the width the other columns leave, and the table does not scroll sideways

#### Scenario: A long answer list wraps within its cap

- **GIVEN** a DNS name answered by more addresses than fit the answers cap on one line
- **WHEN** the DNS tab lists it
- **THEN** its answers wrap onto further lines within the cap, and the table does not widen past it
