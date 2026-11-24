## ADDED Requirements

### Requirement: The host detail shows the events the agent dropped

The host detail's health conditions SHALL show an agent's `event_capture` component as "Event capture", with its status and message, so an operator sees that a host lost telemetry and how much.

#### Scenario: Dropped events are shown with their count

- **GIVEN** a host whose agent reports `event_capture` as `degraded` because it dropped events
- **WHEN** the operator opens the host's health details
- **THEN** an "Event capture" condition shows the message with the number of events dropped, and the host's attention dot is amber
