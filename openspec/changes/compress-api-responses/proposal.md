## Why

The server sent every response uncompressed, and nothing in front of it compressed them: Caddy in the quickstart is a plain reverse proxy, and the laptop stack has no proxy at all. On dogfood (v0.9.0-rc.5) a long agent session's Timeline was a 20 MB JSON response and its process tree 9 MB, so most of each tab's load was spent moving bytes. Gzip takes the Timeline to about 1.5 MB (issue #1550).

## What changes

- The server's HTTP middleware compresses a response with zstd or gzip when the client's `Accept-Encoding` allows it and the body is large enough to gain, and marks it `Vary: Accept-Encoding`.
- Each compressed body is padded by a few bytes derived from its content, so its exact length cannot be used to recover a secret in it (BREACH).
- The agent control channel's gRPC stream is dispatched before this middleware and is not affected.

## Impact

- `server/httpserver`: one new layer in `Build`, used by the server and ingest binaries alike.
- `github.com/klauspost/compress` becomes a direct dependency; it was already in the module graph.
