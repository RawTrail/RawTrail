## ADDED Requirements

### Requirement: Agent sessions list cost per page

The agent sessions list SHALL read each listed session's owner, the uid of its earliest retained process, from that session's processes in index order, stopping at the first, so the owner read does not grow with the number of processes a session holds.

#### Scenario: A session with many processes does not slow the page

- **GIVEN** an agent session holding thousands of processes, and other hosts' processes that began earlier
- **WHEN** the plan of the owner lookup that the list runs per row is read
- **THEN** it reads processes by the session index, not by fork time
- **AND** it does not sort the session's processes
