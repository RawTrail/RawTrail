## ADDED Requirements

### Requirement: Shipped alert rules read emitted metrics

The project SHALL ship its recommended alert rules as importable definitions rather than prose, and every metric a shipped rule reads SHALL be one that the service named in the rule's filter emits. A rule whose metric is renamed, removed, or emitted only by another service evaluates to no data, which reads exactly like a healthy system, so the mismatch MUST fail the build instead.

#### Scenario: A rule reads a metric its service emits

- **GIVEN** a shipped alert rule that reads an `edr.*` metric and filters on a `service.name`
- **WHEN** the build checks the shipped rules
- **THEN** the check fails unless that service's own source emits the metric
