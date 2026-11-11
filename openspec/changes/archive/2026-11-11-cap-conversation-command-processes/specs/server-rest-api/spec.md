## ADDED Requirements

### Requirement: Conversation turns list a large command's processes in part

A page of a session's conversation SHALL list each turn's command of more than 100 processes as a page of the session's commands lists it: its first 100 in its forest's order, every process an alert was raised on and every process the page's search finds, each under its creators, marked `processes_cut`, with `process_ids` naming every process.

#### Scenario: A turn's large command is listed in part

- **GIVEN** a command whose wrapper shell ran make, which started 150 workers, the 141st of them alerted
- **WHEN** the session's conversation is read
- **THEN** the turn holding the command lists its first 100 processes and the alerted worker, marks it cut, and names all 153 processes
