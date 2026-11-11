## 1. Compression

- [x] 1.1 Add a zstd and gzip compression layer to `httpserver.Build`, with content-derived length jitter
- [x] 1.2 Test that a large response is compressed for gzip and zstd clients and left as is otherwise

## 2. Verification

- [ ] 2.1 On dogfood, the session's Timeline is served with `Content-Encoding` and its load time falls
