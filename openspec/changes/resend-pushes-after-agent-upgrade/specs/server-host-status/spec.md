## ADDED Requirements

### Requirement: A check-in records when the agent changed

A status check-in that reports an agent version other than the one stored SHALL record when it did, on the database clock; a check-in reporting the same version, or none, SHALL leave that time as it was.

#### Scenario: An upgrade in place is recorded

- **GIVEN** a host enrolled with agent 0.4.0
- **WHEN** it checks in with 0.4.0, then with 0.5.0, then with 0.5.0 again
- **THEN** no change is recorded for the first, the second records one, and the third leaves it as the second set it
