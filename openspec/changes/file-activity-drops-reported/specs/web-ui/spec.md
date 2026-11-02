## ADDED Requirements

### Requirement: An agent session says when its file activity is incomplete

A session's Summary and the card it is shared as SHALL say how many file operations were not recorded when its digest counts any, and SHALL say nothing of it otherwise.

#### Scenario: The Summary says how many file operations were not recorded

- **GIVEN** a session whose digest counts 309 unrecorded file operations
- **WHEN** its Summary is shown
- **THEN** it says 309 file operations were not recorded, so its file counts leave them out
- **AND** the card notes "309 operations not recorded" under its files changed
