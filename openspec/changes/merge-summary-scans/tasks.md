## 1. Fewer, concurrent reads

- [x] 1.1 Count a scope's activity totals and its timeline in one ClickHouse pass, re-bucketing only when an event falls outside the span
- [x] 1.2 Run the Summary's reads that do not need the session's processes beside the ones that do
- [x] 1.3 Test the totals from the digest, that the one-pass and widened timelines agree, and that the reads overlap

## 2. Verification

- [ ] 2.1 On dogfood, the long session's Summary loads in a fraction of its 11 s and ClickHouse's query log shows one totals pass
