## ADDED Requirements

### Requirement: Activity table cells stay on one line

Each cell of an activity table (Files, Network, DNS) SHALL keep its value on one line. When the table is wider than its frame, the frame SHALL scroll sideways rather than wrap the value.

#### Scenario: A file path in the process detail panel reads whole

- **GIVEN** a process whose file activity names a path longer than the panel's path column would otherwise allow
- **WHEN** the operator opens its Files activity in the process detail panel at an 800 pixel wide viewport
- **THEN** the path's cell is one line tall
