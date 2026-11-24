## 1. Agent

- [x] 1.1 Tell a receiver's drop sink of every dropped event, and wire both macOS receivers to health and a metric.
- [x] 1.2 Report the `event_capture` component from the drops, degraded for an hour after the last.
- [x] 1.3 Count drops on `edr.agent.receiver.dropped` by service.

## 2. Console

- [x] 2.1 Label `event_capture` as "Event capture".

## 3. Tests

- [x] 3.1 Health: no component before a drop, degraded with counts and prompt report, healthy after an hour, a new spell after that.
- [x] 3.2 The sink is told of every drop; the metric counts by service; the agent's drop reporter feeds both; the console shows the condition.

## 4. Verify

- [x] 4.1 Overflow a receiver on edr-dev and see the drop count in the agent's health and on the host page.
