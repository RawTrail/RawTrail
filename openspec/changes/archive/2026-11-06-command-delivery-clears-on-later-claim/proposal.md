## Why

A host stayed Degraded with "it is not taking commands" after it had started taking them again (#1387). It was powered off for days, three queued commands expired when it came back, and three later commands completed within seconds. An hour later the host page still said it was not taking commands, because any expiry inside the 24 hour window raised the condition whatever happened afterwards. Every laptop back from a long sleep shows this.

## What changes

- An expired command stops counting toward the `command_delivery` condition once the host has claimed a later command. Expiries after the latest claim still count.

## Impact

- `server/response/internal/mysql/store.go`: `UndeliverableByHost`.
