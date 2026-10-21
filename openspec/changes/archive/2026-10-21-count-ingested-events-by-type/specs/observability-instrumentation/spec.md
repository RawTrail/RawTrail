## ADDED Requirements

### Requirement: Ingested events carry a bounded event type

The system SHALL attribute `edr.events.ingested` with `event_type` in addition to `host_id`. The value SHALL be one of the event types the event schema defines, and any other value SHALL be recorded as `other`, so the attribute's cardinality is fixed whatever an agent sends.

#### Scenario: Ingested events are counted by event type

- **GIVEN** the ingest endpoint accepts a batch holding events of several schema event types for a host
- **WHEN** the batch is committed
- **THEN** `edr.events.ingested` is incremented for each event type by the number of events of that type, with `host_id` and `event_type` attributes

#### Scenario: An unknown event type is counted as other

- **GIVEN** the ingest endpoint accepts an event whose type the event schema does not define
- **WHEN** the batch is committed
- **THEN** `edr.events.ingested` records it with `event_type` set to `other`

#### Scenario: The bounded set matches the event schema

- **GIVEN** the set of event types the server keeps for the attribute
- **WHEN** it is compared with the event type enumeration in the event schema
- **THEN** the two contain the same values
