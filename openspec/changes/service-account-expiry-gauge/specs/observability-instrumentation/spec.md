## ADDED Requirements

### Requirement: Expiring service accounts are a gauge

The system SHALL expose an observable gauge, `edr.service_accounts.expiring`, reporting the number of service accounts that are neither revoked nor expired and whose credential expires within 14 days. It MUST be evaluated on each collection cycle, and a failed callback MUST NOT take down the collection cycle for other gauges.

#### Scenario: Expiring accounts are counted

- **GIVEN** an active service account created with a 7 day lifetime, an active one with a 90 day lifetime, a revoked one with 7 days left, and one already expired
- **WHEN** the OTel reader requests a metric collection
- **THEN** `edr.service_accounts.expiring` reports 1
