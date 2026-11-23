## 1. Agent

- [x] 1.1 Report a plain `/name args` user entry with no origin or prompt source as a prompt when it follows the local-command caveat entry.

## 2. Tests

- [x] 2.1 Reader cases: the command after its caveat; the same text with no caveat, after another entry, or after a meta entry that is not a caveat; text that is not a slash command; an agent-origin line; an interactive `/clear`; and the command still reported with another session's caveat between it and its own.

## 3. Verify

- [x] 3.1 Run the reader over a real `claude -p "/code-review ..."` transcript from a Mac and confirm one prompt reading `/code-review`, source `sdk`, with its turn.
