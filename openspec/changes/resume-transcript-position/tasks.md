## 1. Agent

- [x] 1.1 Keep an idle transcript's read position, bounded at 1,024 positions with the least recently changed forgotten first.
- [x] 1.2 Drop an idle position's sent-message set, and keep it for a recent transcript a pass did not read.

## 2. Tests

- [x] 2.1 A session resuming after an idle break sends only what it added.
- [x] 2.2 The bound forgets the least recently changed, and an idle position keeps its offset, turn and mode.

## 3. Verify

- [x] 3.1 Run the reader over a real Claude Code transcript copied from a Mac: read it, let it go idle, append a line, and confirm one event and only the added bytes are read.
