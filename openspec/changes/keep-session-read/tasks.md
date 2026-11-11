## 1. Session read cache

- [x] 1.1 Keep a session's process read for 30 seconds, keyed on the session and its last activity, with concurrent misses sharing one read
- [x] 1.2 Bound the cache to four reads and test reuse, expiry, a moved session, failure, sharing and the bound

## 2. Verification

- [ ] 2.1 On dogfood, a second page of a long session's Timeline and a second tab answer faster than the first
