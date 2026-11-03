## 1. Queue reads

- [x] 1.1 Read the never-claimed arm of the host hint as a loose index scan
- [x] 1.2 Claim each claim state by its own index-ordered read and merge them oldest first

## 2. Tests

- [x] 2.1 Pin both reads' plans over a seeded backlog
- [x] 2.2 A claim merges never-claimed and expired events in event order
