## 1. Extension

- [x] 1.1 Add an optional `pidversion` to the exit, open, rename, truncate and delete payloads.
- [x] 1.2 Fill it from the acting process's audit token at every emit site.

## 2. Schema and tests

- [x] 2.1 Document `pidversion` on the five payloads in `schema/events.json`.
- [x] 2.2 Swift tests pin the wire key, its omission and the round trip.
- [x] 2.3 The server exit payload decodes `pidversion`, pinned by a property-based round trip.

## 3. QA

- [x] 3.1 On edr-dev, an exit's and a watched-path file event's `pidversion` equal the actor's exec `pidversion`.
