## Why

When an agent sends an oversized frame, gRPC writes `ResourceExhausted` to the client itself. A heartbeat the gateway was sending at that moment then fails on the ended stream and records an outbound-failure teardown, and the gateway reported that teardown instead. The client received `RESOURCE_EXHAUSTED` while the connection's span recorded `UNAVAILABLE` (#1626, seen in CI under load).

## What Changes

- **Gateway:** a rejected frame's status ends the RPC whatever teardown is recorded alongside it, since the client has already been sent it. When the connection ends because a send failed, the gateway waits for the receive result, as it already did when the stream ended under it, instead of reporting the send failure at once.
- **Spec:** ADDED "A rejected frame outranks the failures it causes" (`agent-control-channel`).

## Impact

- `server/response/internal/gateway/gateway.go` and its tests.
