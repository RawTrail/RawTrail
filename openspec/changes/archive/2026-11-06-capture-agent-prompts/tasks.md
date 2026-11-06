## 1. Agent

- [x] 1.1 Read the prompts a person typed, from the origin Claude Code records, and a slash command as typed.
- [x] 1.2 Redact recognizable credentials and cut the text to 16 KiB on a character boundary.
- [x] 1.3 `prompt_id` on tool calls; `EDR_CAPTURE_PROMPTS=0` turns prompt capture off.
- [x] 1.4 Schema, metrics label set, operator docs.

## 2. Tests

- [x] 2.1 Parser cases from real transcript shapes (2.1.309), unreadable lines, redaction kinds, wire round-trip property, fuzz.
- [ ] 2.2 Live macOS VM: a real Claude Code session's prompts arrive on the dev server, a pasted fake key is redacted, the switch off stops them.
