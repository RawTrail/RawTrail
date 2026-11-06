## Why

`TestAnOrdinaryDisconnectIsNotRecordedAsAFailedConnection` failed intermittently (#1329), and so did production telemetry: an ordinary client disconnect sometimes ended the `Connect` span in Error with `client disconnected`. Under net/http's HTTP/2 server the pending receive can fail with a gRPC status before the stream context is cancelled, and a status error was read as a fault.

A first fix waited up to 250 ms for the context to be cancelled before reporting a status. That was wrong: gRPC cancels the context moments after it rejects a frame, so a frame the server could not read ended as a clean connection (#1405).

## What changes

- A failed receive is read from its status code. Unavailable and Canceled mean the client or its transport went away and end the RPC cleanly. Any other status is gRPC rejecting a frame and stays a fault, even when the context is already cancelled. The wait is gone.

## Impact

- `server/response/internal/gateway/gateway.go`: `endAfterReceive`.
