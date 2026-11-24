## Why

The macOS agent drops security- and network-extension events when the channel between its XPC receiver and its enqueue loop is full, and the only record was a rate-limited local log line (#1584). Dropping rather than blocking is deliberate, since a blocked Endpoint Security client is killed, but the silence meant an operator never learned a host had lost telemetry. A v0.9.0-rc.10 laptop logged 66 such warnings in four days, up to about 11,700 events in one burst.

## What changes

- The agent counts every receiver drop against its extension and reports an `event_capture` health component: `degraded` with reason `events_dropped` for an hour after the last drop, then `healthy` with reason `no_recent_drops`, its message giving the count per extension since the agent started and the last drop's time. The first drop of a burst is reported promptly. A host that never dropped anything reports no such component.
- Each drop is also counted on the OTel counter `edr.agent.receiver.dropped`, by `service`.
- The console labels the component "Event capture". The server is unchanged: component types and reasons are open vocabulary, and a degraded component does not open a health episode.
- Reducing how many events are dropped is a separate change (#1584, part two), measured first.

## Impact

- `agent/health/drops.go`, `agent/health/health.go`, `agent/receiver/{common,receiver,receiver_other}.go`, `agent/metrics/metrics.go`, `agent/cmd/rawtrail-agent/{main,sensors,sensors_notwindows}.go`, `ui/src/components/HostHeader.tsx`.
