## ADDED Requirements

### Requirement: Dropped events are reported in health

When an extension delivers an event the agent cannot take because its receiver channel is full, the agent SHALL count the drop against that extension and report an `event_capture` component: `degraded` with reason `events_dropped` for an hour after the last drop, then `healthy` with reason `no_recent_drops`, its message giving the count per extension since the agent started and the last drop's time. It SHALL also count each drop on a metric by extension.

#### Scenario: A burst the agent could not keep up with is reported

- **GIVEN** an agent whose security extension delivers events faster than the agent takes them, so some are dropped
- **WHEN** the agent reports its health
- **THEN** the `event_capture` component is `degraded` with reason `events_dropped`, its message naming how many events each extension dropped and when the last was, and the first drop of a burst is reported without waiting for the periodic report
- **AND** each drop is counted on `edr.agent.receiver.dropped` with the extension as its `service`

#### Scenario: Event capture recovers after a quiet hour

- **GIVEN** an agent whose last dropped event was more than an hour ago
- **WHEN** it reports its health
- **THEN** the `event_capture` component is `healthy` with reason `no_recent_drops`, dated from the end of that hour, and still gives the count since the agent started
- **AND** an agent that has dropped nothing reports no `event_capture` component at all
