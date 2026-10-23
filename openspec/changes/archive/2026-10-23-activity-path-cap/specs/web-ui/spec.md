## MODIFIED Requirements

### Requirement: Activity table cells stay on one line

Each cell of an activity table (Files, Network, DNS) SHALL keep its value on one line. A file path SHALL be capped in width and cut at its start, so its file name stays in view, with the whole path as its tooltip; a rename's two paths SHALL each be capped. When the table is still wider than its frame, the frame SHALL scroll sideways rather than wrap the value.

#### Scenario: A file path in the process detail panel reads whole

- **GIVEN** a process whose file activity names a path longer than the panel's path column would otherwise allow
- **WHEN** the operator opens its Files activity in the process detail panel at an 800 pixel wide viewport
- **THEN** the path's cell is one line tall

#### Scenario: A long path is cut at its start and shown whole on hover

- **GIVEN** a file row whose path is longer than the cap, and a rename between two such paths
- **WHEN** the Files tab lists them
- **THEN** each path is capped, the rename's two at half the cap each, and each carries its whole path as its tooltip
- **AND** the cell's text is still the whole path, a rename's as its source, an arrow and its destination
