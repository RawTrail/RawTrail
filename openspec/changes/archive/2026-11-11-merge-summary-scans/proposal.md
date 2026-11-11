## Why

A long AI agent session's Summary took 11 to 12 seconds on dogfood (v0.9.0-rc.5). Its trace and ClickHouse's query log show why (issue #1550):

- The reads ran one after another: the session's processes from MySQL (about 1.3 s), then its activity totals (2.6 s), its tool calls, usage and prompts (0.7 s together), then its digest.
- The digest's timeline read the session's whole scope once to find its first and last event (4.8 s) and then again to bucket it (1.3 s).
- The activity totals read that same scope a third time. Each of the three reads went through about 4.6 million events.

## What changes

- The Summary's reads that do not need the session's processes (its process and alert counts, tool calls, usage and prompt count) run beside the read of its processes and the digest that needs them, so the Summary waits for its slowest read instead of the sum of them.
- The digest's timeline is bucketed in the same pass that totals the session's activity, over the session's own span. Only when one of the scope's events falls outside that span does it bucket the events again over the widened span, as before.
- The archive's separate totals read for a Summary is removed: the digest carries the totals.

## Impact

- `server/visibility`: `ActivityDigest` gains `Totals`, and `EventArchive.ActivityTotalsForGenerations` is removed. The Activity tab's totals are unchanged.
- `server/detection/internal/operator`: `sessionSummary` runs its reads in an errgroup.
- The Summary's response is unchanged.
