## ADDED Requirements

### Requirement: Folded agent file rows name their process count

The Files tab and the host timeline SHALL say how many processes an agent file row folds, when it folds several.

#### Scenario: A fan-out's row names how many processes it counts

- **GIVEN** a file row folded from 80 processes under `xargs`
- **WHEN** the Files tab or the host timeline shows it
- **THEN** the Files tab names "80 processes under" the process, and the timeline spells the operation "by 80 processes"
