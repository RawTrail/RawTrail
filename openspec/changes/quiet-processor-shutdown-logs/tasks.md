## 1. Server

- [x] 1.1 The processor logs each failed queue, lock or store call through one helper that downgrades a failure caused by its own cancellation to DEBUG
- [x] 1.2 Test: a cancellation caused by shutdown logs at DEBUG; a cancellation with the worker still live, and a real failure during shutdown, both log at ERROR
- [x] 1.3 Live: stopping a lane B dev server that is draining a backlog logs no ERROR lines from the processor
