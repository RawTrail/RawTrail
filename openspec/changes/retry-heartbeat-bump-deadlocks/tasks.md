## 1. Server

- [x] 1.1 Each heartbeat bump chunk runs through `sqlhelpers.WithDeadlockRetry`
- [x] 1.2 Test: a bump forced to lose a real InnoDB deadlock against a concurrent transaction still applies, and the concurrent transaction commits
