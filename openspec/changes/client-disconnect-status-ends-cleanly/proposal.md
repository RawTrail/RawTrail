## Why

`TestAnOrdinaryDisconnectIsNotRecordedAsAFailedConnection` failed intermittently (#1329), and so did production telemetry: an ordinary client disconnect sometimes ended the `Connect` span in Error with `client disconnected`. Under net/http's HTTP/2 server the pending receive can fail with a gRPC status before the stream context is cancelled, and a status error was read as a fault.

## What changes

- A receive that fails with a status waits up to 250 ms for the RPC context to be cancelled. A cancel means the client went away and the RPC ends cleanly. A status on a context that stays live is still reported.
- The 250 ms only bounds how long a status error with a live context is held before it is reported.

## Impact

- `server/response/internal/gateway/gateway.go`: `endAfterReceive`.
