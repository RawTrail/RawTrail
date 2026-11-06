## ADDED Requirements

### Requirement: Session commands list their processes in part

A page of a session's commands SHALL list a command of more than 100 processes in part: its first 100 in its forest's order, every process an alert was raised on and every process the page's search finds, each under its creators, marked `processes_cut`, with `process_ids` naming every process. `?command=<key>` SHALL return that one command with every process listed, or `404 not_found` when the session holds no such command.

#### Scenario: A large command lists its first processes and its alerted ones

- **GIVEN** a command whose wrapper shell ran make, which started 150 workers, the 141st of them alerted
- **WHEN** a page of the session's commands is read, and again with a search for the 146th worker
- **THEN** the command lists its first 100 processes and the alerted worker, is marked cut, and names all 152 processes
- **AND** with the search, the 146th worker is listed too

#### Scenario: One command is read whole on request

- **GIVEN** the same command
- **WHEN** it is read by its key
- **THEN** every one of its processes is listed and it is not marked cut
- **AND** a key the session does not hold is answered with `404 not_found`
