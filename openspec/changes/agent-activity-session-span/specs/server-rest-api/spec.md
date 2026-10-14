## ADDED Requirements

### Requirement: Agent activity bounded to its session

A session's activity SHALL count the agent process's records, by their first occurrence, only from its first touch of a path named after the session, its transcript or one of its directories, to its first touch of a path of a later session; the first session it served SHALL also keep the process's start. An agent that touched no other session's paths, or none of this session's, SHALL be counted whole.

#### Scenario: A reused agent counts in each session only while serving it

- **GIVEN** one `claude` process that served session A, then after `/clear` session B, creating B's scratch directory a few milliseconds before its transcript and writing A's transcript once more as A closed
- **WHEN** each session's activity is read
- **THEN** A counts the agent's activity up to B's scratch directory, and B from it on
- **AND** an agent that served one session only is counted whole
