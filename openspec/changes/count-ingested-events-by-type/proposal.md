## Why

`edr.events.ingested` is split only by host, so metrics cannot show which event types a deployment receives (issue #1217). Rules run only when a batch holds their event type, so a rule that never evaluates could mean the sensor sends no events of that type, and nothing told the two apart. On prod, the TCC rule recorded no evaluations in 72 hours with no way to see whether any TCC events arrived.

## What changes

- `edr.events.ingested` carries an `event_type` attribute alongside `host_id`.
- Its values are the event types `schema/events.json` defines. Anything else is recorded as `other`, so the attribute's cardinality is fixed whatever an agent sends. A test keeps the server's copy of the set equal to the schema's.

## Not changed

The counter's name, its `host_id` attribute, and its total are unchanged: summing over `event_type` gives the same series as before.
