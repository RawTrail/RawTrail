## MODIFIED Requirements

### Requirement: Agent sessions list cost per page

The agent sessions list SHALL read each listed session's owner, the uid of its earliest retained process, from that session's processes in index order, stopping at the first. It SHALL count each listed session's processes up to 10,000 and report a count that reached the bound as capped. Neither read SHALL grow with the number of processes a session holds.

#### Scenario: A session with many processes does not slow the page

- **GIVEN** an agent session holding thousands of processes, and other hosts' processes that began earlier
- **WHEN** the plan of the owner lookup that the list runs per row is read
- **THEN** it reads processes by the session index, not by fork time
- **AND** it does not sort the session's processes

#### Scenario: A session's process count stops at its bound

- **GIVEN** a session with one process more than the bound, a session with exactly the bound, and a session with 3 processes
- **WHEN** the sessions list is read
- **THEN** the first reports the bound and says it is capped, the second reports the bound and is not capped, and the third reports 3
- **AND** counting a session past the bound reads at most the bound and one more of its index entries
