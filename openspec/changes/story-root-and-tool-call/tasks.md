## 1. Graph builder

- [x] 1.1 Stamp `story_root_id` and `tool_call_group` on every write path, in the overlay and the flush.
- [x] 1.2 Resolve same-batch story roots, and self roots, after the batched INSERT with the creator links.

## 2. Tests

- [x] 2.1 Integration tests on a captured session shape: story roots, launchd-started and first-seen-at-exec roots, tool calls through daemonizing and `setsid`.
- [x] 2.2 Property test: each process's story root is its nearest launchd-started ancestor. The differential test compares both keys.
