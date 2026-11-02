## 1. Fold

- [x] 1.1 Settle each folded group on its own path when it merged nothing, and on its rows' shared directory when it did
- [x] 1.2 Key the window by the settled path, so a later operation on it joins the row

## 2. Tests

- [x] 2.1 A pipeline of 80 processes on one file keeps the file's path; a group of two files settles on their directory
- [x] 2.2 Property: a rolled-up row folds more than one path, and any other row names a path operated on
- [x] 2.3 Exercise on edr-dev with a busy pipeline
