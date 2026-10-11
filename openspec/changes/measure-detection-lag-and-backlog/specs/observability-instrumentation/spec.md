## ADDED Requirements

### Requirement: Pipeline lag and backlog are measured

The server SHALL record `edr.detection.lag`, a histogram of each event's seconds from the server receiving it to its batch being acknowledged after detection, recorded only by the attempt whose acknowledgement succeeded. `fleet-edr-server` SHALL report `edr.events.pending`, the count of events waiting for or in detection, stopping at 1,000,000 so the count stays within the gauge's collection budget.

#### Scenario: Detection lag is recorded per processed event

- **GIVEN** a batch of events the server received five seconds ago
- **WHEN** the processor evaluates the batch and its acknowledgement succeeds
- **THEN** one `edr.detection.lag` sample of at least five seconds is recorded per event
- **AND** a batch that is nacked, whose acknowledgement fails, or whose claim was lost records no sample

#### Scenario: Backlog count saturates at its cap

- **GIVEN** a queue holding more pending events than the count's cap
- **WHEN** the pending count is read
- **THEN** it returns the cap rather than counting the whole backlog
- **AND** below the cap it is exact and includes events claimed but not yet acknowledged

### Requirement: Stalled and slow detection raise shipped alerts

The project SHALL ship an alert rule that fires at critical severity when events stay queued while no detection lag is recorded for 15 minutes, and one that fires at error severity when the 95th percentile of detection lag stays above 5 minutes for 10 minutes.

#### Scenario: A stalled pipeline raises a critical alert

- **GIVEN** the shipped "EDR detection stalled" rule
- **WHEN** `edr.events.pending` stays above zero while `edr.detection.lag` records nothing for 15 minutes
- **THEN** the rule fires at critical severity
- **AND** it does not fire while the backlog is zero, so an idle pipeline does not page

#### Scenario: Slow detection raises an error alert

- **GIVEN** the shipped "EDR detection lag high" rule
- **WHEN** the 95th percentile of `edr.detection.lag` stays above 300 seconds for 10 minutes
- **THEN** the rule fires at error severity
