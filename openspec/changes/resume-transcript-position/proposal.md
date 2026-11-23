## Why

The agent re-sent a whole Claude Code transcript every time a session resumed after more than 10 minutes idle (#1545). The transcript reader forgot the read position of any transcript that had not changed within its 10-minute window, so a resumed session's transcript was new to it and was read from its start. On the dogfood laptop one long session's 61 MB transcript was sent in full three times in a night, about 16,000 rows uploaded for 5,444 events. Nothing was visible to users, since each event's id is stable and the archive keeps one row, but the cost was agent CPU, upload volume, server ingest and ClickHouse storage until merges, and a large re-read delayed the session's new lines behind its old ones.

## What changes

- The reader keeps a transcript's position while it is idle, so a resumed session's transcript is read from where the reader stopped.
- The positions kept are bounded at 1,024, the least recently changed forgotten first, dated by when each transcript last changed rather than when it was last read.
- Each position remembers at most the 64 most recent streamed messages whose usage it sent, instead of all of them. That record only stops a message's several entries from each sending its usage, and the entries are written one after another (measured: no other message's entry between them across 32,489 messages in 162 transcripts), so a kept position stays small however long its transcript is idle.
- A transcript replaced at its path is still read from its start, and a restart still reads each recent transcript once from its start.

## Impact

- `agent/agenttranscript/reader.go`. No wire, server or schema change.
