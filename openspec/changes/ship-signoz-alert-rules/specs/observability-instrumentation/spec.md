## ADDED Requirements

### Requirement: Shipped alert rules read emitted metrics

The project SHALL ship its recommended alert rules as importable definitions rather than prose, and every metric a shipped rule reads SHALL be one that the service named in the rule's filter emits. A rule whose metric is renamed, removed, or emitted only by another service evaluates to no data, which reads exactly like a healthy system, so the mismatch MUST fail the build instead.

#### Scenario: A rule reads a metric its service emits

- **GIVEN** a shipped alert rule that reads an `edr.*` metric and filters on a `service.name`
- **WHEN** the build checks the shipped rules
- **THEN** the check fails unless that service's own source emits the metric

### Requirement: Metric temporality is selectable per backend

The server SHALL export metrics with the temporality the standard `OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE` variable selects, defaulting to cumulative, so a deployment reporting to a backend that drops the first cumulative sample (SigNoz, Datadog) can switch to delta. The quickstart compose SHALL forward that variable to the server, and the shipped dashboards SHALL NOT pin a temporality, so they read either kind.

#### Scenario: Compose forwards it and dashboards do not pin it

- **GIVEN** the shipped quickstart compose and dashboards
- **WHEN** the build checks them
- **THEN** the compose passes `OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE` to the server and no dashboard query names a temporality
