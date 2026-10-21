## ADDED Requirements

### Requirement: The ingest service counts the events it accepts

The standalone ingest service SHALL record `edr.events.ingested` and `edr.ingest.heartbeats_dropped` for every batch it accepts, as the server does. It SHALL NOT report the host-fleet gauges, which describe the whole deployment and are reported by the server.

#### Scenario: Events accepted by the ingest service are counted

- **GIVEN** a standalone ingest service
- **WHEN** it accepts a batch of events
- **THEN** `edr.events.ingested` is incremented for the batch under the ingest service's name

#### Scenario: The ingest service reports no host-fleet gauges

- **GIVEN** a standalone ingest service
- **WHEN** its metrics are collected
- **THEN** neither `edr.enrolled.hosts` nor `edr.offline.hosts` is among them
