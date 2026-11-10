## ADDED Requirements

### Requirement: Captured alert evidence is reused

When an alert or monitor record is inserted, a triggering event whose envelope an alert already holds SHALL be copied from that alert's evidence instead of read from the event archive. Only the triggering events no alert holds SHALL be read from the archive.

#### Scenario: Captured evidence is not read from the archive again

- **GIVEN** an alert that captured the evidence of event `ev-1`
- **WHEN** the same finding is inserted again, a second alert cites `ev-1` and `ev-2`, and a monitor record cites `ev-1`
- **THEN** the repeated insert and the monitor record read nothing from the archive, and the second alert reads only `ev-2`
- **AND** the second alert and the monitor record hold `ev-1`'s envelope as the first alert captured it
