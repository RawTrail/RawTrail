## Why

`rawtrail-ingest`, the standalone ingest binary, never built a metrics recorder (issue #1263). Its intake handler therefore counted nothing: in a split deployment, where agents upload to the ingest tier, no replica reported `edr.events.ingested` or `edr.ingest.heartbeats_dropped`, and the ingest tier reported no HTTP request duration. The `EDR event ingest stopped` alert would fire while ingest was healthy.

## What changes

- The ingest binary builds a recorder and hands it to detection, whose intake handler records the ingest counters, and to its HTTP server, which records request duration.
- It registers no host-fleet gauges. `rawtrail-server` already reports `edr.enrolled.hosts` and `edr.offline.hosts` for the whole deployment, and a second copy would double every fleet-wide sum.

## Not changed

`rawtrail-server`'s metrics are unchanged.
