## 1. Server

- [x] 1.1 Send an activity read's generations as an external table, and match the pid before the payload
- [x] 1.2 Remove the raised query size the reads no longer need

## 2. Tests

- [x] 2.1 The 20,000-generation scope reads under ClickHouse's default query size, mutation-checked against tuples in the text
- [x] 2.2 Measure the old and new read on the dogfood database
- [ ] 2.3 Open a long session's Summary and Activity on dogfood after the RC is deployed
