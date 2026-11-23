## 1. Agent

- [x] 1.1 Keep an idle transcript's read position, bounded at 1,024 positions with the least recently changed forgotten first.
- [x] 1.2 Bound each position's sent-message record to its 64 most recent messages, and date positions by the transcript's latest change.

## 2. Tests

- [x] 2.1 A session resuming after an idle break sends only what it added.
- [x] 2.2 The bounds: the least recently changed position is forgotten, and a position keeps its 64 most recent sent messages.
- [x] 2.3 A refused line read after an idle break, and a message streamed across passes, do not send usage twice.

## 3. Verify

- [x] 3.1 Run the reader over a real Claude Code transcript copied from a Mac: read it, let it go idle, append a line, and confirm one event and only the added bytes are read.
