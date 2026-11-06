## Why

A session's header token and permission-mode totals, its tokens in the sessions list, and its "ran in bypass mode" mark count every `agent_usage` line that names the session, whoever's transcript it came from (issue #1478). Another local user on the same Mac can write a transcript naming someone else's session, inflating its tokens or marking it bypassed when it never was. The session lineage already trusts only lines from the session's owner.

## What changes

- A session's usage counts only lines whose `uid` is the session's owner, the uid of its earliest retained process: the header totals, the list's tokens and the bypass mark alike.
- The bypass record keeps the line's uid (migration `00025_agent_session_bypass_uid`), and the list matches it against the owner. Rows recorded before the migration carry no uid and mark nothing; the feature is unreleased.
- A session whose owner is no longer known reports no usage and no bypass mark, as its lineage reports none.

## Impact

- Visibility: `AgentUsageTotals` takes the owner's uid, `AgentSessionRef` carries it.
- Detection: the graph builder's bypass sighting, its flush, the sessions list and the session summary.
