## 1. Server

- [x] 1.1 Order the owner lookup by the whole session index key

## 2. Tests

- [x] 2.1 Pin the owner lookup's plan: it reads the session index and does not sort, mutation-checked against the old ORDER BY
- [x] 2.2 Measure the old and new lookup on the dogfood database
- [ ] 2.3 Open the agent sessions page on dogfood after the fix is deployed
