## 1. Server

- [x] 1.1 Bound a session's activity at its agents' span starts (`scopeStart`)
- [x] 1.2 Judge the read's lower bound by the span-start expression, so the overlap form keeps a straddling event
- [x] 1.3 Keep each agent generation's session touches for 30 seconds per replica, sharing concurrent lookups

## 2. Verification

- [x] 2.1 Unit tests for `scopeStart` with spans, the handler's bound, and the shared touches lookup; mutation-checked
- [x] 2.2 ClickHouse integration test: a drop event straddling the span start counts when the read is bounded at it
- [ ] 2.3 Measure the Activity tab on dogfood after the next RC
