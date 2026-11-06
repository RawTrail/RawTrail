## 1. Server

- [x] 1.1 `UndeliverableByHost` skips an expired command that the host has claimed a later command after.
- [x] 1.2 Store test against MySQL: completed, failed and still-acked later claims clear the condition, an earlier claim does not, and only the expiries after the claim count.
