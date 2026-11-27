## MODIFIED Requirements

### Requirement: Observable host-fleet gauges

The system SHALL expose two observable gauges, `edr.enrolled.hosts` and `edr.offline.hosts`, evaluated on each collection cycle by the OTel reader. The enrolled gauge MUST report the number of non-revoked enrollments. The offline gauge MUST report the number of those enrollments not seen within the configured offline threshold, so it never exceeds the enrolled gauge. A failed callback MUST NOT take down the collection cycle for other gauges.

#### Scenario: Gauges evaluate on the reader cadence

- **GIVEN** OTLP export is enabled
- **WHEN** the OTel reader requests a metric collection
- **THEN** the gauges' callbacks run, query the data store, and observe the current values

#### Scenario: A failing gauge callback is contained

- **GIVEN** the data store is temporarily unavailable
- **WHEN** the gauge callback fails
- **THEN** no value is observed for that gauge in this collection cycle but the rest of the metric collection succeeds

#### Scenario: A revoked host is not counted offline

- **GIVEN** one enrolled host checking in and one host whose enrollment was revoked and which no longer checks in
- **WHEN** the gauges are collected
- **THEN** `edr.enrolled.hosts` is 1 and `edr.offline.hosts` is 0
- **AND** an enrolled host that has not checked in within the threshold, or never has, is counted offline
