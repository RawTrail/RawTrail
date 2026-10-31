## ADDED Requirements

### Requirement: Data collection settings show the store size

The Data collection page SHALL show how much disk the event archive and the database take, their total, and how long events are kept, and SHALL warn, naming what to change, when the server flags the total as past its threshold. A failed read SHALL be reported in that section alone.

#### Scenario: The page shows the store size and retention

- **GIVEN** stores taking 80 GB of events and 30 GB of other data, with events kept 7 days
- **WHEN** an operator opens Data collection
- **THEN** the page says the stores take 110.0 GB, split as 80.0 GB in the event archive and 30.0 GB in the database, and that events are kept for 7 days

#### Scenario: The page warns past the threshold

- **GIVEN** a report the server flags as past its 100 GB threshold
- **WHEN** an operator opens Data collection
- **THEN** the page warns that the stores are past 100.0 GB and names `EDR_EVENT_RETENTION_DAYS`
