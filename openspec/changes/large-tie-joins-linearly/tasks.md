## 1. Join

- [x] 1.1 Return a time group as the list's own slice, kept until a call of the group is pinned; keep empty scans and joined whole and cut ties
- [x] 1.2 Find a tie by its shared slice, and keep no copy of a tie

## 2. Tests

- [x] 2.1 The four quadratic shapes join correctly at full size, with a benchmark of each
- [x] 2.2 A one-off differential over 30,000 random sessions found the same labels with and without the kept scans
