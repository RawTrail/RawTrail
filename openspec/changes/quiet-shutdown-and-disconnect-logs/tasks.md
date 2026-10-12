## 1. Server

- [x] 1.1 The stop func that `bootstrap.Start` returns now flushes OTel before closing the pool
- [x] 1.2 The ingest handler logs a write its client's disconnect cancelled at INFO and writes no response, including the best-effort heartbeat bump and host upsert; every other write failure stays an ERROR, and a failed archive or queue write still returns 500
- [x] 1.3 Test: the stop func order, with the pool still open at the flush
- [x] 1.4 Test: a disconnect logs no ERROR and writes nothing, during the fan-out and during the best-effort writes against a real store; a cancellation while the request is live and a real failure both log ERROR and return 500
- [x] 1.5 Live: stopping a lane B dev server logs no `gauge callback failed` line, and a client that gives up mid-upload logs INFO lines and no ERROR
