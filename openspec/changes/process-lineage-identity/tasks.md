## 1. Extension

- [x] 1.1 Add `ProcessLineage` to the exec payload, encoded flat, every field omitted when nil.
- [x] 1.2 Read the lineage fields and `cwd` from the ESF exec event.
- [x] 1.3 Add `parent_pidversion` to the fork payload from the forking process's audit token.
- [x] 1.4 Fill `group_id` and `start_time_ns` on startup snapshot rows.

## 2. Schema and tests

- [x] 2.1 Document every new field in `schema/events.json`.
- [x] 2.2 Swift tests pin the wire keys, their omission, the round trip, and the timeval conversion.
- [x] 2.3 A server test pins that exec and fork payloads carrying the new fields decode to exactly what they decode to without them.

## 3. QA

- [x] 3.1 On edr-dev, a double-forked daemon's exec carries `ppid` 1 and its creator in `original_ppid`; its fork carries the creator's `parent_pidversion`; shell execs carry a non-empty `cwd`.
