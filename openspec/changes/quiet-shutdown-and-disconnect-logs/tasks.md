## 1. Server

- [x] 1.1 The stop func `bootstrap.Start` returns flushes OTel before closing the pool
- [x] 1.2 The ingest handler logs a write its client's disconnect cancelled at INFO and writes no response; every other write failure stays an ERROR and a 500
- [x] 1.3 Test: the stop func order, with the pool still open at the flush
- [x] 1.4 Test: a disconnect logs no ERROR and writes nothing; a cancellation while the request is live and a real failure both log ERROR and return 500
- [x] 1.5 Live: stopping a lane B dev server logs no `gauge callback failed` line, and restarting the edr-dev agent mid-upload logs no ERROR
