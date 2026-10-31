## ADDED Requirements

### Requirement: The store size is reported

The server SHALL report, at `GET /api/v1/admin/storage` with `detection_config.read`, the space the event archive's tables and the database's tables take, their total, the event retention in days, and the threshold set by `EDR_STORE_SIZE_WARN_GB` (default 100 GB, 0 never warns), and SHALL flag a total past a non-zero threshold.

#### Scenario: The warning threshold is a setting

- **GIVEN** a server configured without `EDR_STORE_SIZE_WARN_GB`, or with 0, or with a negative value
- **WHEN** it starts
- **THEN** the threshold is 100 GB, never warns, or the server refuses to start, respectively

#### Scenario: The store size and retention are reported

- **GIVEN** an event archive and a database holding data
- **WHEN** the storage report is read
- **THEN** it gives each store's size read from the store itself, their total, and the event retention

#### Scenario: A store past the threshold is flagged

- **GIVEN** stores taking 110 GB together
- **WHEN** the report is read with a threshold of 100 GB, of 200 GB, or of 0
- **THEN** it is flagged only at 100 GB
