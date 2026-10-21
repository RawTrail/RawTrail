## Why

The recommended alerts lived only as prose in the operations guide, so every deployment had to hand-build them, and nothing noticed when a metric they named was renamed or never emitted by the service they assumed. One recommended alert watched the server for a counter only the agent records.

## What changes

- Nine SigNoz alert rules ship under `config/observability/alerts/`, with `tools/signoz-alerts.sh` to apply them.
- A test checks that every metric a rule reads is emitted by the service the rule filters on.
- The server no longer registers `edr.agent.queue.dropped`, which only the agent records; the server's copy had no caller. No rule watches agent drops, because agents export OTel only for debugging, so the server has no production signal for them yet.
- The quickstart compose forwards `OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE` and `OTEL_EXPORTER_OTLP_COMPRESSION`, and the install guide recommends delta temporality for SigNoz and Datadog.

## Not changed

No metric, span or attribute name changes. The exporter's default temporality stays cumulative.
