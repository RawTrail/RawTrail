## ADDED Requirements

### Requirement: Agent file rows folded from several processes

The activity rollup SHALL list a `file_activity` event that carries `process_count` apart from the named process's own operations on the same path, as a row whose `processes` sums the events' `process_count`.

#### Scenario: A row folded from several processes is listed with their count

- **GIVEN** a process's own change to a file, and two events folding 80 and then 40 of its children's changes to that file
- **WHEN** its activity is read
- **THEN** the file is listed twice: once for the process's own change, and once weighing every folded change with `processes` 120
- **AND** a page of one row continues from the first to the second
