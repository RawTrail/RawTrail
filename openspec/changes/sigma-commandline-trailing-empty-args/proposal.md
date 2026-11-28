## Why

The imported Sigma rule `proc_creation_macos_space_after_filename` matched every exec whose last argument is empty (#1635). Sigma's `CommandLine` is built by joining argv with single spaces, so an empty last argument left a trailing space that is in no argument the process received, and the rule matches `CommandLine|endswith: ' '`.

iTerm2's `pidinfo` helper is exec'd every couple of seconds as `pidinfo --git-state <dir> 4 1 ""`. On one Mac that was about 1,800 monitor records an hour. Each record is kept for seven days and keeps its process row from the retention purge: 300,000 records and 1.6 million process rows on a laptop install whose MySQL is capped at 1 GiB.

## What changes

- `CommandLine` leaves out the empty arguments at the end of argv. An exec whose arguments are all empty supplies no `CommandLine`, like one with no arguments.
- Empty arguments elsewhere are joined as before, and every other field is unchanged.
- The rule still matches what it is for (T1036.006): an executable path ending in a space, or a last argument that ends in one.
- A rule whose value ends in a space (`' -d '`) no longer matches a flag followed only by empty arguments. That is how it already treats a flag that comes last.

## Impact

- `server/rules/internal/sigmabind/event.go`. No wire, schema or stored data change.
- Monitor records already written are pruned on their usual seven-day schedule.
