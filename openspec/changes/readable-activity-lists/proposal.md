## Why

The Activity tab's lists are hard to read for a long session (issue #1561). A DNS name answered by many addresses lists them all on one line and pushes the table sideways, the DNS list cannot be ordered by name, and every list states when a row was first and last seen as a full timestamp, where the Agent sessions list shows an age ("6m ago") with the exact time on hover.

## What changes

- A DNS row's answers wrap within a capped width. Every other activity cell still keeps to one line.
- The DNS list's Name header sorts the list by name, and back to when each was first seen.
- First and Last read as an age, with the exact time on hover, on the Files, Network and DNS lists.

## Impact

- `ui/src/components/Activity`: the Activity tabs of the session view and of process detail. The API is unchanged.
