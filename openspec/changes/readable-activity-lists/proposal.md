## Why

The Activity tab's lists are hard to read for a long session (issue #1561). A DNS name answered by many addresses lists them all on one line and pushes the table sideways, a name is listed once per process that resolved it (on dogfood `api.github.com` took 32 rows), the DNS list cannot be ordered by name, and every list states when a row was first and last seen as a full timestamp, where the Agent sessions list shows an age ("6m ago") with the exact time on hover.

## What changes

- A DNS row's answers wrap within a capped width. Every other activity cell still keeps to one line.
- A DNS name is one row over every process that resolved it: its count, first and last are the name's, and its process cell names the first 16 processes, wrapped, with how many others resolved it.
- The DNS list's Name header sorts the list by name, and back to when each was first seen.
- First and Last read as an age, with the exact time on hover, on the Files, Network and DNS lists.

## Impact

- `ui/src/components/Activity`: the Activity tabs of the session view and of process detail.
- The activity API's DNS rows are one a name. A row keeps `pid` and `pidversion`, as its first process's, and adds `resolvers` and `process_count`.
