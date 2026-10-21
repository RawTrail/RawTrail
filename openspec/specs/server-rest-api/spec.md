# Server REST API Specification

## Purpose

The server REST API is the read and triage surface that the admin web UI and any first-party automation use to inspect the EDR's state. It exposes the materialized host inventory, the per-host process forest, the per-process detail (including network connections, DNS queries, and same-PID re-exec chains), and the persisted alerts produced by the detection engine. It also lets an operator update the lifecycle status of an alert.

The capability is the contract between the backend and any browser client: every field a client renders comes from these endpoints, and the JSON shapes here are stable across non-breaking releases. Endpoints described in this capability are the session-authenticated UI surface; agent-facing endpoints (event ingestion, command polling) live in their own capabilities.

## Requirements

### Requirement: Session authentication and CSRF protection

The system SHALL require a valid session cookie on every endpoint defined in this capability. For unsafe methods (`POST`, `PUT`, `DELETE`) the system MUST additionally require a matching CSRF token header. Requests that fail either check MUST be rejected before any business logic executes.

#### Scenario: A browser without a session cookie calls a UI endpoint

- **GIVEN** a client that does not present a valid `edr_session` cookie
- **WHEN** the client calls any endpoint defined in this capability
- **THEN** the system responds with HTTP 401 and an error body
- **AND** no host, process, or alert data is returned

#### Scenario: A state-changing call omits the CSRF token

- **GIVEN** a client with a valid session cookie
- **WHEN** the client issues `PUT /api/alerts/{id}` without a matching `X-CSRF-Token` header
- **THEN** the system responds with HTTP 403 and the alert is not modified

### Requirement: List enrolled hosts

The system SHALL expose `GET /api/hosts` returning a JSON array of enrolled hosts. Each entry SHALL include the host identifier, the count of events seen for that host, the most recent timestamp at which any event from the host was observed, and the host's enrollment hostname, operating-system version, and platform. The hostname, OS version, and platform SHALL be sourced from the host's enrollment record; a host that has sent events but has no enrollment record SHALL still appear in the array with an empty hostname, an empty OS version, and an empty platform rather than being omitted.

The change from the prior requirement is the addition of the host platform to each entry, sourced by the same join on the shared host identifier as the hostname and OS version, with an empty value (not omission) for an un-enrolled host.

#### Scenario: An operator opens the hosts dashboard

- **GIVEN** a logged-in operator
- **WHEN** the client calls `GET /api/hosts`
- **THEN** the system responds with HTTP 200 and a JSON array
- **AND** every entry contains the host identifier, an event count, and a last-seen timestamp

#### Scenario: Host list rows carry enrollment hostname and OS version

- **GIVEN** one host with an enrollment record (hostname and OS version) and one host that has sent events but has no enrollment record
- **WHEN** the client calls `GET /api/hosts`
- **THEN** the enrolled host's entry carries its enrollment hostname and OS version
- **AND** the un-enrolled host still appears with an empty hostname and an empty OS version

#### Scenario: Host list rows carry the host platform

- **GIVEN** one host with an enrollment record naming its platform and one host that has sent events but has no enrollment record
- **WHEN** the client calls `GET /api/hosts`
- **THEN** the enrolled host's entry carries its enrollment platform
- **AND** the un-enrolled host still appears with an empty platform

### Requirement: Per-host process forest

The system SHALL expose `GET /api/hosts/{host_id}/tree` returning the process forest for that host. The response SHALL nest each process under its parent and SHALL attach the network connections and DNS queries that occurred during each process's lifetime. By default the response SHALL collapse repeated identical-path leaf siblings under the same parent into aggregated nodes, each carrying the group's count, its exited-versus-running split, its first and last fork times, and a capped sample of the underlying members, nested in the forest exactly where the collapsed siblings sat. The endpoint SHALL accept an optional `flatten` boolean query parameter; when set it returns the raw, un-aggregated forest with every node. The endpoint SHALL also accept an optional `pin` query parameter naming a single process by its id; that process SHALL be kept a first-class node, never folded into a sibling aggregate, so a client such as the alert view can always locate the alerted process by its real id even when it has identical siblings.

The response SHALL additionally carry result metadata describing what the read did NOT return: `total_matched`, the count of process rows whose lifetime overlaps the requested window; `returned`, the count of rows the limit admitted, before any aggregation folded them; `total_matched_capped`, whether that count is a floor rather than a total; and `truncated`, whether the limit dropped rows. `total_matched` SHALL be counted with the same window predicate that selects the rows, so the two can never disagree. A client MUST be able to report what is missing using only these fields, without re-deriving the server's effective limit.

`truncated` SHALL be established by the read itself, by fetching one row beyond the requested limit, and SHALL NOT be derived from `total_matched`. The count behind that number is bounded and may give up, so deriving truncation from it reports a partial forest as complete on exactly the hosts where the count cannot finish, which is where a partial forest is most likely. A read proven untruncated SHALL skip the count entirely, since it holds every row that matched, and SHALL NOT set `total_matched_capped`.

Counting SHALL be bounded both by a row bound and by a time budget, and `total_matched` is therefore exact only when the count completed within both AND came back above the rows returned; it is a floor in every other case, with `total_matched_capped` saying which. The last of those conditions is not pedantry: the count and the row read are separate statements, so retention pruning between them can return a total below the rows already in hand, and "showing 2,000 of 1,998" under a truncation notice is not a claim the server can stand behind. A count that exhausts its time budget SHALL NOT fail the request: the rows are already in hand and the request is answerable without the number, so the rows returned become the floor and the response reports "more than what you can see". The row bound caps rows emitted rather than rows examined, so a window matching fewer rows than the bound still walks its whole range to prove it; the time budget is the backstop for that, and it is why a bound alone is not sufficient. An unbounded count is what this replaces, and it took the endpoint down: on a dogfood host carrying 5.2 million process rows, a 24-hour window matched 542,268 of them and the count ran past 120 seconds against a 30-second server write timeout, so the request returned 500 and the operator saw a failed graph. The row read itself was never slow, at about 0.1 seconds for the same window, because its `ORDER BY fork_time_ns DESC LIMIT` lets the scan stop early. Counting SHALL use that same access path so its cost is bounded by the bound rather than by how much the window matched.

The exactness given up buys a request that always answers. A denominator past the bound tells an operator nothing they act on differently: the decision a truncated tree drives is to narrow the window, and "more than 10,000" drives it as well as "542,268" does. A number that sometimes takes two minutes and then fails is worth less than a bounded one that always arrives.

#### Scenario: An operator views a host's process tree

- **GIVEN** a logged-in operator and a known host with recorded activity
- **WHEN** the client calls `GET /api/hosts/{host_id}/tree`
- **THEN** the system responds with HTTP 200 and a JSON object containing the forest of root processes
- **AND** each process node carries its child processes and the network connections and DNS queries linked to it

#### Scenario: A time range is supplied

- **GIVEN** a logged-in operator
- **WHEN** the client calls `GET /api/hosts/{host_id}/tree` with optional `from` or `to` nanosecond bounds
- **THEN** the response is restricted to processes whose lifetime overlaps the specified window

#### Scenario: Repeated identical siblings collapse into an aggregated node

- **GIVEN** a logged-in operator and a host where a parent spawned many childless children of the same image path and binary identity
- **WHEN** the client calls `GET /api/hosts/{host_id}/tree` without `flatten`
- **THEN** those siblings appear as a single aggregated node carrying the group count, the exited-versus-running split, the first and last fork times, and a capped sample of the members

#### Scenario: An operator opts out of aggregation with flatten

- **GIVEN** a logged-in operator viewing a host whose tree contains aggregated groups
- **WHEN** the client calls `GET /api/hosts/{host_id}/tree` with the `flatten` parameter set
- **THEN** the response contains the raw forest with every sibling as its own node and no aggregated nodes

#### Scenario: A pinned process is never folded into an aggregate

- **GIVEN** a logged-in operator and a host where the pinned process has one or more identical-image sibling leaves that would otherwise collapse into an aggregated node
- **WHEN** the client calls `GET /api/hosts/{host_id}/tree` with `pin` set to that process's id
- **THEN** the pinned process appears as its own first-class node carrying its real id, not folded into any aggregated node

#### Scenario: A window matching more processes than the limit is reported as truncated

- **GIVEN** a logged-in operator and a host whose window overlaps more process rows than the effective row limit
- **WHEN** the client calls `GET /api/hosts/{host_id}/tree`
- **THEN** the response sets `truncated` to true
- **AND** `total_matched` equals the true count of overlapping rows, not the limit, when that count is at or below the counting bound and completes
- **AND** `returned` equals the number of rows the limit admitted

#### Scenario: A window inside the limit is not reported as truncated

- **GIVEN** a logged-in operator and a host whose window overlaps fewer process rows than the effective row limit
- **WHEN** the client calls `GET /api/hosts/{host_id}/tree`
- **THEN** the response sets `truncated` to false
- **AND** `returned` equals `total_matched`

#### Scenario: The reported total ignores the requested limit

- **GIVEN** a logged-in operator and a host whose window overlaps a fixed number of process rows, and a count that completes
- **WHEN** the client calls `GET /api/hosts/{host_id}/tree` twice with different `limit` values
- **THEN** both responses report the same `total_matched`
- **AND** the one exception is a count that could not complete, whose floor is the rows THAT request returned and therefore moves with the limit

#### Scenario: Counting stops at its bound rather than scanning the whole window

- **GIVEN** a host whose window overlaps far more process rows than the counting bound
- **WHEN** the client calls `GET /api/hosts/{host_id}/tree`
- **THEN** `total_matched` equals the counting bound rather than the true number of overlapping rows
- **AND** `total_matched_capped` is true
- **AND** the response is returned rather than failing on a server timeout
- **AND** a window matching exactly the counting bound reports that number with `total_matched_capped` false, because nothing lies beyond it
- **AND** a count that exhausts its time budget reports the rows returned as the floor, with `total_matched_capped` true and `truncated` still true

### Requirement: Per-process detail with re-exec chain

The system SHALL expose `GET /api/hosts/{host_id}/processes/{pid}` returning a single process record together with its network connections, DNS queries, and the ordered re-exec chain of prior generations on the same PID.

The endpoint SHALL accept an optional `pidversion` query parameter naming one generation of that PID. When it is supplied the system MUST return that exact generation, resolved by `(host, pid, pidversion)` identity, and MUST respond 404 when no generation of the PID carries it, rather than falling back to a different generation. When it is absent the system MUST resolve the generation from the `at` instant as before. Naming the generation is the only way to address any but the newest member of a re-exec chain, because a re-exec preserves the chain's original fork time so all its generations share one, and the as-of resolution can therefore only ever reach the most recently recorded one. A malformed `pidversion` MUST be rejected with HTTP 400 rather than ignored, so a bad value cannot silently return a different generation than the caller asked for.

The network connections and DNS queries returned SHALL be those belonging to the SPECIFIC process generation the response describes, not to every generation that shared the PID. A flow whose payload carries a `pidversion` SHALL be attributed by `(pid, pidversion)` identity, and the system MUST NOT additionally require its INGEST time to fall inside the generation's lifetime window: a flow and its process's exit reach the server in separate uploads, so a short-lived process's flow routinely ingests after its own exit, and requiring that window drops exactly the flows an alert most often fired on. The system MAY require the flow's EVENT time to fall inside the generation's life, padded for the drift between the flow's clock and the process's, and MUST do so where a `pidversion` is not unique across the PID's generations: records written before generations recorded their own generation repeated one value across a re-exec chain, and without a time bound each such generation would serve every other one's flows.

A flow whose payload carries no `pidversion` SHALL be attributed by an ingest-time window derived from the generation's lifetime, because identity is unavailable for it; that window MUST tolerate a flow arriving a full upload cycle after the exit rather than only a reordering within one upload. A flow carrying a `pidversion` that matches no generation of that PID MUST NOT be attributed to any generation by timing proximity. When the process generation ITSELF carries no `pidversion`, only flows that also carry none are attributed to it: a flow that names a generation belongs to whichever generation it names, and a record that cannot name its own must not claim it on timing alone.

The scan the read performs MUST NOT be bounded by a span measured from the process's own start, because a process older than that span would then have every one of its flows excluded no matter how they were attributed. The response SHALL be bounded in size, and when the bound drops rows the response MUST report that it was truncated rather than returning a silently shortened list, so an absent flow is not read as a flow that never happened.

#### Scenario: An operator inspects a process detail

- **GIVEN** a logged-in operator and a host plus PID with recorded activity
- **WHEN** the client calls `GET /api/hosts/{host_id}/processes/{pid}`
- **THEN** the system responds with HTTP 200 and a JSON object containing the process record, its network connections, and its DNS queries
- **AND** when the process has prior generations on the same PID the response carries those generations in oldest-first order as the re-exec chain

#### Scenario: The PID is not known on the host

- **GIVEN** a logged-in operator
- **WHEN** the client calls `GET /api/hosts/{host_id}/processes/{pid}` for a PID that has no record on that host
- **THEN** the system responds with HTTP 404 and an error body

#### Scenario: A flow that ingests after its process exited is still attributed to it

- **GIVEN** a process generation that has exited, and a flow carrying that generation's `pidversion` whose ingest time is several seconds after the generation's exit ingest time
- **WHEN** the client requests that generation's detail
- **THEN** the response carries that flow among its network connections
- **AND** the attribution does not depend on the flow's ingest time falling inside the generation's lifetime window

#### Scenario: A sibling generation of the same PID does not claim the flow

- **GIVEN** two generations of one PID with different `pidversion`s, one of which owns a flow by identity, and a sibling that has not exited
- **WHEN** the client requests the sibling generation's detail
- **THEN** the response does not carry the other generation's flow
- **AND** requesting the owning generation's detail does carry it

#### Scenario: A named generation of a re-exec chain is addressable

- **GIVEN** a PID with two generations from a re-exec, which therefore share one fork time, the older of which owns a flow by identity
- **WHEN** the client requests that PID's detail supplying the older generation's `pidversion`
- **THEN** the response describes the older generation and carries its flow
- **AND** requesting the same PID and instant WITHOUT a `pidversion` describes the newest generation instead, which is the only generation the as-of resolution can reach

#### Scenario: A pidversion naming no generation is not silently substituted

- **GIVEN** a PID with recorded generations on a host
- **WHEN** the client requests that PID's detail supplying a `pidversion` that no generation of it carries
- **THEN** the system responds with HTTP 404 rather than describing a different generation
- **AND** a `pidversion` that is not a parseable unsigned 32-bit value is rejected with HTTP 400

#### Scenario: A flow without a kernel generation is attributed by the lifetime window

- **GIVEN** a flow whose payload carries no `pidversion`, recorded against a PID whose generation has a known lifetime
- **WHEN** the client requests that generation's detail
- **THEN** the response carries that flow when its ingest time falls inside the generation's window
- **AND** a flow carrying `pidversion` 0 is treated as carrying a real kernel generation rather than as carrying none

#### Scenario: A repeated generation is disambiguated by event time

- **GIVEN** two generations of one PID that share a `pidversion`, as records written before generations recorded their own do, each owning one flow made during its own life
- **WHEN** the client requests each generation's detail in turn
- **THEN** each response carries only the flow made during that generation's life
- **AND** neither carries the other's, even though identity alone cannot separate them

#### Scenario: A capped flow read reports that it truncated

- **GIVEN** a process generation whose attributed flows exceed the response's row bound
- **WHEN** the client requests that generation's detail
- **THEN** the response carries at most the bound's worth of flows
- **AND** the response reports that it was truncated, so the client can tell a bounded list from a complete one

#### Scenario: A generation that carries no kernel generation claims only flows that carry none

- **GIVEN** a process record with no `pidversion`, and two flows on that PID, one carrying a `pidversion` and one carrying none, both inside the record's lifetime window
- **WHEN** the client requests that record's detail
- **THEN** the response carries only the flow that carries no `pidversion`
- **AND** it does not carry the flow that names a generation, since that flow belongs to the generation it names

### Requirement: Filterable alerts list

The system SHALL expose `GET /api/alerts` returning a JSON array of detection alerts. The response SHALL be filterable by host identifier, status, severity, source, rule identifier, linked process identifier, and disposition.

The `source` filter selects alerts by which subsystem raised them, so an operator can separate application-control blocks from catalog-rule detections without reading every row. Filters combine conjunctively: an alert must satisfy every filter supplied.

The `disposition` filter selects between alerts and monitor records. It SHALL default to `alert`, so a caller that does not ask for monitor records never receives one and no existing client changes behaviour. `disposition=monitor` SHALL return monitor records only, and any other value SHALL be rejected with HTTP 400. Each entry SHALL report its disposition.

#### Scenario: An operator filters alerts by host

- **GIVEN** a logged-in operator
- **WHEN** the client calls `GET /api/alerts?host_id=H`
- **THEN** the system responds with HTTP 200 and a JSON array
- **AND** every entry's host identifier equals `H`

#### Scenario: An operator combines status and severity filters

- **GIVEN** a logged-in operator
- **WHEN** the client calls `GET /api/alerts?status=open&severity=critical`
- **THEN** the response includes only alerts whose status is `open` and whose severity is `critical`

#### Scenario: An operator filters alerts by source

- **GIVEN** a deployment with alerts from more than one source
- **WHEN** the client calls `GET /api/alerts?source=application_control`
- **THEN** the response includes only alerts whose source is `application_control`
- **AND** alerts raised by catalog detection rules are excluded

#### Scenario: The list excludes monitor records unless they are asked for

- **GIVEN** a deployment with alerts and monitor records
- **WHEN** the client calls `GET /api/alerts` with no disposition
- **THEN** every entry has disposition `alert`
- **AND** `GET /api/alerts?disposition=monitor&rule_id=R` returns only monitor records of rule `R`

#### Scenario: An unknown disposition is rejected

- **GIVEN** a logged-in operator
- **WHEN** the client calls `GET /api/alerts?disposition=everything`
- **THEN** the system responds with HTTP 400 and an error body

### Requirement: Alert detail with linked event ids

The system SHALL expose `GET /api/alerts/{id}` returning a single alert or monitor record. The response SHALL include its host identifier, rule identifier, disposition, severity, title, description, linked process identifier, MITRE ATT&CK technique identifiers, status, and the list of event identifiers that triggered it. A monitor record is served here like an alert so an operator can open it and pivot from it, and the disposition is what tells a client not to offer triage on it.

The change from the prior requirement is that the endpoint also serves monitor records and reports the disposition.

#### Scenario: An operator opens an alert

- **GIVEN** a logged-in operator and an existing alert
- **WHEN** the client calls `GET /api/alerts/{id}`
- **THEN** the system responds with HTTP 200 and a JSON object
- **AND** the object includes the rule identifier, severity, title, description, linked process identifier, technique identifiers, status, and the list of triggering event identifiers

#### Scenario: The alert id is unknown

- **GIVEN** a logged-in operator
- **WHEN** the client calls `GET /api/alerts/{id}` with an identifier that does not exist
- **THEN** the system responds with HTTP 404 and an error body

#### Scenario: The detail of a monitor record reports its disposition

- **GIVEN** a logged-in operator and an existing monitor record
- **WHEN** the client calls `GET /api/alerts/{id}` for it
- **THEN** the system responds with HTTP 200, and the object reports disposition `monitor` along with its triggering event identifiers

### Requirement: Update alert lifecycle status

The system SHALL expose `PUT /api/alerts/{id}` accepting a JSON body that sets the alert status to one of `open`, `acknowledged`, or `resolved`. Any other status value MUST be rejected. On success the system MUST record which authenticated user performed the change. A monitor record SHALL NOT be triaged, so a status change addressed to one MUST be rejected and the record left unchanged.

#### Scenario: An operator resolves an alert

- **GIVEN** a logged-in operator and an existing alert
- **WHEN** the client issues `PUT /api/alerts/{id}` with body `{"status": "resolved"}`
- **THEN** the system responds with HTTP 204
- **AND** the alert's stored status becomes `resolved`
- **AND** the identity of the operator that performed the change is recorded

#### Scenario: An invalid status value is supplied

- **GIVEN** a logged-in operator
- **WHEN** the client issues `PUT /api/alerts/{id}` with a status that is not one of `open`, `acknowledged`, or `resolved`
- **THEN** the system responds with HTTP 400 and the alert is not modified

#### Scenario: A status change addressed to a monitor record is rejected

- **GIVEN** a logged-in operator and an existing monitor record
- **WHEN** the client issues `PUT /api/alerts/{id}` for it with body `{"status": "acknowledged"}`
- **THEN** the system responds with HTTP 400 and the monitor record is not modified

### Requirement: JSON response format and error shape

The system SHALL return successful response bodies as `application/json` with a UTF-8 encoding. Error responses SHALL be returned as JSON with a single `error` field whose value is a stable typed error code (for example `{"error": "unauthorized"}`) so clients can branch on the code without parsing free-form prose. The same `ErrorResponse` shape MUST be used for every 4xx and 5xx response defined in this capability.

#### Scenario: An endpoint returns an error

- **GIVEN** a logged-in operator
- **WHEN** any endpoint defined in this capability fails with a 4xx or 5xx status
- **THEN** the response body is a JSON object with an `error` field carrying a stable typed error code
- **AND** clients can dispatch on the code without further parsing

#### Scenario: A successful response is JSON

- **GIVEN** any successful call to an endpoint defined in this capability
- **WHEN** the response body is non-empty
- **THEN** the `Content-Type` is `application/json` and the body parses as valid JSON

### Requirement: Registered authed routes are reachable through the composed router

Every authed API route a bounded context registers SHALL be reachable through the composed outer router with its session-authentication boundary applied. The set of session-protected routes SHALL be derived from what the contexts register rather than maintained as a separate hand-edited allowlist, so a registered authed route can never be omitted from the protected surface. An unauthenticated request to a registered authed route SHALL receive the session middleware's JSON authentication failure, never a fall-through to the single-page-app HTML catch-all.

#### Scenario: A registered authed route is session-protected, not SPA fall-through

- **GIVEN** a bounded context registers an authed API route
- **WHEN** an unauthenticated request hits that route through the composed router
- **THEN** the response is the session-authentication failure as JSON
- **AND** it is not the single-page-app HTML catch-all

### Requirement: Fleet-wide connection and DNS search endpoints

The system SHALL expose `GET /api/search/connections` and `GET /api/search/dns`, each authorized by the process-read action, returning events from the archive across all hosts: connections whose remote address equals a requested value, and DNS queries whose query name equals it. Each endpoint SHALL accept the artifact value (optional; when absent or empty the endpoint lists the most recent events of that type across the fleet rather than rejecting the request, mirroring the process search), an optional `host_id` scoping the search to one host, and a `from`/`to` window over ingest time. Results SHALL be ordered newest-first, paginated by an opaque keyset cursor over `(timestamp_ns, event_id)` such that paging the full set yields every matching event exactly once with no skips or duplicates, and each response SHALL carry the page of events, a `next_cursor` when more remain, and `total_matched`. For the recent-events listing (no artifact value) the endpoint MAY report `total_matched` as not computed (a negative sentinel) to avoid an expensive fleet-wide count; a filtered (artifact-valued) search SHALL report the exact `total_matched`. Pagination is driven by `next_cursor`, so it does not depend on the total. A malformed cursor SHALL be rejected with 400.

#### Scenario: Connection search finds a remote address across hosts

- **GIVEN** two hosts that each connected to the same remote address, plus connections to other addresses
- **WHEN** the client calls `GET /api/search/connections` with that address
- **THEN** the response contains the matching connection from each host and no others
- **AND** `total_matched` equals that count

#### Scenario: DNS search finds a query name across hosts

- **GIVEN** DNS queries for a domain on more than one host, plus queries for other domains
- **WHEN** the client calls `GET /api/search/dns` with that domain
- **THEN** the response contains only the matching queries, drawn from every host

#### Scenario: Host filter scopes the search

- **GIVEN** a fleet-wide artifact match set
- **WHEN** the client adds a `host_id`
- **THEN** the response contains only that host's matching events

#### Scenario: Absent artifact value lists recent events

- **GIVEN** a request with no artifact value
- **WHEN** the endpoint processes it
- **THEN** it returns the most recent events of that type across the fleet, newest-first, with no artifact filter applied
- **AND** it does not compute a fleet-wide total, reporting `total_matched` as the not-computed sentinel

#### Scenario: Keyset pagination is stable and complete

- **GIVEN** a matching set larger than one page
- **WHEN** the client pages through it following `next_cursor` until none is returned
- **THEN** the concatenation of pages is every matching event exactly once, newest-first, with no duplicate or skipped event

### Requirement: Fleet-wide process search endpoint

The system SHALL expose `GET /api/search/processes` returning processes matching a set of composable filters across all hosts, authorized by the process-read action. Supported filters, all optional and combined with AND in the database query (not in application code): `host_id` (exact; absent means every host), `path` (substring), `hash` (exact SHA-256), `uid` (exact), a `from`/`to` fork-time window, `exit_reason` (exact), and `signing` (a derived signer class: `unsigned`, `ad-hoc`, `platform`, `developer-id`, or `signed`). A `signing` or `exit_reason` value outside its accepted vocabulary SHALL be rejected with 400 rather than silently applied as a filter that matches nothing, so an analyst can trust that a typed filter was understood. Results SHALL be ordered newest-first by fork time with the row identifier breaking ties, and paginated by an opaque keyset cursor such that paging through the full result set yields every matching row exactly once with no skips or duplicates even as new processes are ingested concurrently. Each response SHALL carry the page of rows, a `next_cursor` when more rows remain (absent or empty on the last page), and `total_matched`, the count of all rows matching the filters independent of pagination. For the fully-unfiltered fleet browse (no filter set) the endpoint MAY report `total_matched` as not computed (a negative sentinel) to avoid an expensive count over the whole process table; any filter (even a lone `host_id` or time window, whose count is index-cheap) restores the exact count. Pagination is driven by `next_cursor`, so it does not depend on the total. A malformed cursor SHALL be rejected with 400.

#### Scenario: Filters compose across hosts

- **GIVEN** processes on several hosts, some unsigned, some with uid 0
- **WHEN** the client calls `GET /api/search/processes` with `signing=unsigned` and `uid=0`
- **THEN** the response contains exactly the processes that are both unsigned and uid 0, drawn from every host
- **AND** `total_matched` equals that count

#### Scenario: Unfiltered browse skips the total count

- **GIVEN** a fully-unfiltered request (no filter set)
- **WHEN** the endpoint serves the fleet browse
- **THEN** it returns a page of the newest rows and a `next_cursor` when more remain
- **AND** it reports `total_matched` as the not-computed sentinel rather than counting the whole table
- **AND** adding any filter (for example a single `host_id`) makes it report the exact `total_matched` again

#### Scenario: Hash search spans hosts

- **GIVEN** the same binary hash executed on two different hosts
- **WHEN** the client searches by that `hash` with no `host_id`
- **THEN** the response includes the matching process from each host

#### Scenario: Host filter scopes to one host

- **GIVEN** a fleet-wide match set
- **WHEN** the client adds a `host_id` filter
- **THEN** the response contains only that host's matches, using the same endpoint and contract

#### Scenario: Keyset pagination is stable and complete

- **GIVEN** a filtered result set larger than one page
- **WHEN** the client pages through it following `next_cursor` until none is returned
- **THEN** the concatenation of pages is every matching row exactly once, newest-first, with no duplicate or skipped row

#### Scenario: Malformed cursor is rejected

- **GIVEN** a request carrying a cursor that does not decode
- **WHEN** the endpoint processes it
- **THEN** the response status is 400

#### Scenario: An out-of-vocabulary filter value is rejected

- **GIVEN** a request whose `signing` or `exit_reason` value is not in its accepted set
- **WHEN** the endpoint processes it
- **THEN** the response status is 400 and the reader is not queried

### Requirement: Host detail endpoint

The system SHALL expose `GET /api/hosts/{host_id}` returning one host's identity and liveness: the host identifier, the enrollment hostname, the OS product name, OS version, and OS build, the agent version, the source IP recorded at enrollment, the enrollment time, the timestamp of the inventory report that last refreshed identity, the most recent time any event from the host was observed, the count of events seen, and the server-computed agent-health rollup. The endpoint SHALL be authorized by the same host-read action as the host list. An unknown host identifier MUST return 404; a host that has sent events but has no enrollment record MUST still return, with empty identity fields, matching the list endpoint's posture.

#### Scenario: Operator fetches host detail

- **GIVEN** an enrolled host that has sent events and posted an inventory check-in
- **WHEN** the client calls `GET /api/hosts/{host_id}`
- **THEN** the response carries the hostname, OS name/version/build, agent version, source IP, enrolled-at, last-seen, event count, and health rollup for that host

#### Scenario: Unknown host id returns 404

- **GIVEN** a host identifier no host row matches
- **WHEN** the client calls `GET /api/hosts/{host_id}`
- **THEN** the response status is 404

#### Scenario: Never-enrolled host returns empty identity

- **GIVEN** a host that has sent events but has no enrollment record
- **WHEN** the client calls `GET /api/hosts/{host_id}`
- **THEN** the response returns with empty identity fields rather than 404

### Requirement: Host event timeline endpoint

The server SHALL expose `GET /api/hosts/{host_id}/timeline`, gated on the process-read action scoped to the host, returning that host's exec, network-connection, and DNS-query events interleaved in event-time order, newest first. The endpoint SHALL bound results to an event-time window (`from`/`to` in nanoseconds; zero or absent means unbounded on that side) and SHALL accept an optional `type` filter (a comma-separated subset of the supported event classes) and an optional case-insensitive `text` substring match against the event payload, and an optional `chain` filter scoping results to a set of process generations (each given as `pid:pidversion`), so a client can scope the timeline to an alert's process chain. An event matches a generation when its originating pid and kernel pid generation (pidversion) both match, which uniquely identifies the generation across PID reuse that a raw-pid filter would conflate. An event that does not carry a pidversion SHALL match no generation: a missing pidversion is not pidversion 0 (a real kernel generation), so a scope for `(pid, 0)` MUST NOT include events that never carried the field. A malformed `chain` value SHALL be rejected as a bad request. Results SHALL be keyset-paginated with an opaque cursor and SHALL carry the total match count independent of the page, matching the fleet-wide search contract. An unrecognized event type SHALL be rejected as a bad request; a malformed cursor SHALL be rejected as a bad request; and when the archive read surface is not configured the endpoint SHALL return service-unavailable.

#### Scenario: Timeline interleaves the three event classes in time order

- **GIVEN** a host has exec, network-connection, and DNS-query events within a window
- **WHEN** the timeline is requested for that host and window with no type filter
- **THEN** the response lists all three event classes interleaved in descending event-time order
- **AND** it carries the total number of matching events

#### Scenario: Type filter restricts the classes returned

- **WHEN** the timeline is requested with a type filter naming only DNS queries
- **THEN** only DNS-query events are returned and the total reflects that class alone

#### Scenario: Text match filters by payload substring

- **GIVEN** events whose payloads contain a distinctive string (a path, address, or query name)
- **WHEN** the timeline is requested with that string as the text filter
- **THEN** only events whose payload contains the string (case-insensitively) are returned

#### Scenario: Chain scope selects generations by pid and pidversion

- **GIVEN** a host where a pid was reused by more than one process generation across the window
- **WHEN** the timeline is requested with a `chain` filter of `pid:pidversion` generations
- **THEN** only events whose pid and pidversion match one of those generations are returned, so a later process that reused the same pid is excluded
- **AND** an event that omits pidversion is excluded even from a `(pid, 0)` generation, so events that never carried the field are not swept in
- **AND** a malformed `chain` value is rejected as a bad request

#### Scenario: Keyset pagination is stable and complete

- **GIVEN** a window with more matching events than one page
- **WHEN** the timeline is paged through by following the returned cursor
- **THEN** every matching event appears exactly once across the pages in a stable order
- **AND** the last page returns no further cursor

#### Scenario: An unrecognized event type is rejected

- **WHEN** the timeline is requested with a type filter naming an unsupported event class
- **THEN** the request is rejected as a bad request rather than returning an empty result

### Requirement: Host activity histogram endpoint

The system SHALL expose `GET /api/hosts/{host_id}/activity-histogram` accepting a `from`/`to` nanosecond window and returning the count of process starts per time bucket within it, together with the bucket size. The server SHALL derive the bucket size from the window so the number of buckets stays bounded regardless of the window's width. The bucket counts MUST sum to the total number of process starts in the window, and the endpoint SHALL be authorized by the same host-read action as the process tree.

#### Scenario: Bucketed counts cover the window

- **GIVEN** a host with process starts spread across a requested window
- **WHEN** the client calls the activity-histogram endpoint
- **THEN** the response carries per-bucket counts whose sum equals the number of process starts in the window
- **AND** each start is counted in the bucket containing its start time

#### Scenario: Bucket size scales with the window

- **GIVEN** two requests whose windows differ by an order of magnitude
- **WHEN** each response is returned
- **THEN** each derives a bucket size that keeps the bucket count bounded rather than returning one bucket per fixed interval

#### Scenario: Invalid window is rejected

- **GIVEN** a request whose `from` is not before its `to`
- **WHEN** the client calls the endpoint
- **THEN** the response status is 400

### Requirement: An alert's chain can be read on its own

The process-forest endpoint SHALL offer a read scoped to one process's chain: that process, its ancestors, and its descendants, and nothing else. A client reading a single alert is asking what that process did and what it came from, and answering with the host's activity over a window costs rows in proportion to how busy the host was rather than to the size of the answer.

A chain read SHALL name the process it is the chain of. Asked for a chain with no process named, the system SHALL read the host's window instead, because a caller that named no process has described the host.

A chain read SHALL NOT count the host's window, and SHALL NOT report the window as truncated. Those describe what a windowed read left out, and a chain read left nothing out because it read no window. Reporting them anyway would tell an analyst that the chain in front of them is part of something larger that was cut short, which is not what happened.

Descendants SHALL be bounded by each process's own lifetime. A process number is reused only once its holder has exited, so a process forked after this one exited belongs to whichever process took the number next; attributing it here would show an analyst activity the process never spawned, under its name.

Descendants SHALL be capped, and a chain read SHALL report truncation only about that cap. They are the one direction with no natural bound: ancestors are bounded by the depth of a process tree, while a single process may spawn without limit.

A chain read naming a process that is not stored SHALL return an empty chain rather than the host's forest. Retention removes processes while the alerts raised on them remain, and answering with the host's activity is the fallback this read exists to remove.

#### Scenario: A chain read returns the chain and nothing else

- **GIVEN** a host carrying unrelated activity alongside a process's chain
- **WHEN** the chain is read for that process
- **THEN** the result holds that process, its ancestors and its descendants
- **AND** none of the host's unrelated activity

#### Scenario: A chain read reports no window truncation

- **GIVEN** a chain read that completed
- **WHEN** its result metadata is inspected
- **THEN** it does not report the read as truncated
- **AND** it does not report a capped count of a window it did not read

#### Scenario: Descendants are capped

- **GIVEN** a process that spawned more descendants than the cap admits
- **WHEN** its chain is read
- **THEN** no more descendants than the cap are returned
- **AND** the process and its ancestors are still returned, because the cap bounds the one direction that has no bound of its own
- **AND** the read reports itself as truncated

#### Scenario: A chain read for a missing process is empty

- **GIVEN** a chain read naming a process that is not stored
- **WHEN** the read completes
- **THEN** the result is empty rather than the host's forest

### Requirement: A pinned process is in the page with its ancestors

A process-forest read naming a process to pin SHALL return that process, and every ancestor back to its root, whatever the row limit admitted. A client pins a process because that process is the reason it is reading, so a page that does not contain it has not answered the request, however many other rows it carries.

The row limit alone cannot satisfy this. The read returns the newest rows in the window, so a host busy enough to fill the page after the pinned process forked pushes that process off the page entirely, and does so more reliably the longer an analyst waits before opening the alert.

The ancestors SHALL come with it rather than the pinned row alone. A forest links a child to its parent only among the rows it was given, so a pinned process whose parent is absent is returned as a root: a process presented as having no parent, when it has one the read simply did not fetch.

Each ancestor SHALL be resolved by the same rule the system uses everywhere else to decide which generation of a pid was running at a given instant, because a pid is reused and a second rule for one caller would answer differently from the rest of the system for the same process. The walk SHALL be bounded, so that data claiming a process is its own ancestor cannot make the read run forever.

The counts describing the read SHALL continue to describe the page the limit admitted, not the page plus what the pin added. They exist to tell a client what the read did not return, and a count that silently absorbed the pinned rows would report a page larger than the limit allowed.

A pinned process that no longer exists SHALL NOT fail the read. Retention removes processes while the alerts raised on them remain, and a host's forest is still worth returning to an analyst whose alert has outlived its process.

#### Scenario: The pinned process survives a page of newer activity

- **GIVEN** a host where more processes forked after the pinned process than the row limit admits
- **WHEN** the forest is read for a window containing both, pinning that process
- **THEN** the pinned process is in the result

#### Scenario: Its ancestors come with it

- **GIVEN** the same read
- **WHEN** the result is inspected
- **THEN** every ancestor of the pinned process back to its root is present, so it is returned as part of its chain rather than as a root

#### Scenario: Without a pin the limit still decides the page

- **GIVEN** the same host and window, read without pinning anything
- **WHEN** the result is inspected
- **THEN** it holds only what the row limit admitted

#### Scenario: The counts still describe the page

- **GIVEN** a read whose pin added rows the limit had excluded
- **WHEN** the result metadata is inspected
- **THEN** the count of rows returned is the number the limit admitted
- **AND** the read is still reported as truncated

#### Scenario: A pinned process that no longer exists is not an error

- **GIVEN** a pin naming a process that is not stored
- **WHEN** the forest is read
- **THEN** the read succeeds and returns the host's forest

### Requirement: The graph says where retained process records begin

The process-tree endpoint SHALL report the moment before which completed process records have been deleted by retention, so a client can tell a window whose earlier processes aged out from one in which they never existed. It SHALL NOT report one when process retention is disabled, since nothing has been deleted.

#### Scenario: A window past retention is told where records begin

- **GIVEN** a deployment that deletes completed process records after seven days
- **WHEN** a client reads a host's process tree
- **THEN** the response reports a boundary seven days before now

#### Scenario: No boundary when retention is disabled

- **GIVEN** a deployment with process retention disabled
- **WHEN** a client reads a host's process tree
- **THEN** the response reports no boundary

### Requirement: Agent activity bounded to its session

A session's activity SHALL count the agent process's records, by their first occurrence, only from its first touch of a path named after the session, its transcript or one of its directories, to its first touch of a path of a later session; the first session it served SHALL also keep the process's start. An agent that touched no other session's paths, or none of this session's, SHALL be counted whole.

#### Scenario: A reused agent counts in each session only while serving it

- **GIVEN** one `claude` process that served session A, then after `/clear` session B, creating B's scratch directory a few milliseconds before its transcript and writing A's transcript once more as A closed
- **WHEN** each session's activity is read
- **THEN** A counts the agent's activity up to B's scratch directory, and B from it on
- **AND** an agent that served one session only is counted whole

### Requirement: Agent file activity reads

The activity rollup SHALL list `file_activity` events with the watched-path file events, taking the operation from the payload, weighing each event by its `coalesced_count`, ending a row at its latest `last_timestamp_ns`, and marking a directory row `rolled_up`. A session's activity SHALL include the creator of a root whose pid is the session's agent pid. The host timeline SHALL include `file_activity` among its event classes.

#### Scenario: Agent file activity is totalled by the operations it stands for

- **GIVEN** a process generation with a `modify` of one file folded three times, the same `modify` once more, a single `rename`, and a rolled-up `create` of a directory standing for 1856 files
- **WHEN** its activity is read
- **THEN** the files total counts 1861 operations on 3 paths
- **AND** the file's row counts 4 operations from its first to its last
- **AND** the directory's row is marked `rolled_up`

#### Scenario: The timeline lists agent file activity

- **GIVEN** a host with an exec, a `file_activity` event and a watched-path `open` event
- **WHEN** its timeline is read with no type filter, then with `type=file_activity`
- **THEN** the first read returns the exec and the file activity but not the `open`
- **AND** the second returns the file activity alone

#### Scenario: A session's activity includes the agent's own edits

- **GIVEN** a Claude Code session whose tool calls' shells were created by the `claude` process, whose pid the session's `CLAUDE_PID` marker names
- **WHEN** the session's activity is read
- **THEN** the `claude` process's generation is among those read, once however many calls it started
- **AND** a root created by any other process does not bring its creator in

### Requirement: Agent own processes

A session's tree read SHALL return, as each agent node's `children`, the agent process's descendants that carry no session marker, forked while it served the session, without walking into a process stamped with a session, at most 2,000 per agent, and SHALL set `agent_own_truncated` when cut. `GET /api/alerts?agent_process=&served_session=` SHALL select the alerts on the same processes.

#### Scenario: A session read lists the agent's own processes under it

- **GIVEN** an agent that ran a plugin scan, a shell snapshot with a child and a fork daemon, a tool call with its child, a forked session's process under the daemon, and a scan after the span it served the session
- **WHEN** the session's tree is read
- **THEN** the agent's children are the scan, the snapshot with its child and the daemon, and not the tool call, the forked session's process or the later scan

#### Scenario: An agent's own processes' alerts are read by the agent

- **GIVEN** alerts on an agent's plugin scan, on its shell snapshot's child, on a scan after the session's span, and on the session's tool call
- **WHEN** the alerts are listed with `agent_process`, `served_session` and `host_id`, or without the host or the session
- **THEN** the first two are returned, not the later scan's or the tool call's, and without either the request is rejected with `agent_process_requires_host_id_and_served_session`

### Requirement: Copied agent tool calls

A tool call whose `origin_session_id` or `forked_from_session_id` names a session on the same host that holds a call with the same `tool_use_id` and owner SHALL be treated as a copy of that call. The session's tool calls SHALL NOT list a copy.

#### Scenario: A copied call is listed under its origin only

- **GIVEN** a fork holding a copy of its parent's call, and a session whose own call names a session that never held it, as after `/clear`
- **WHEN** each session's tool calls are read
- **THEN** the copy is listed under the parent only, and the other call under its own session, as is another user's line naming the parent

### Requirement: Agent session lineage endpoint

`GET /api/hosts/{host_id}/agent-sessions/{session_id}/lineage` SHALL return `forked_from_session_id`, the parent the session's fork job names, or else the origin of its latest copied call, and `forked_session_ids`, the sessions whose parent it is, sorted, counting only calls owned by the uid of the session's earliest process and linking only sessions that uid owns. It SHALL be gated on process read for the host and answer 404 for a session the host does not hold.

#### Scenario: A session names its parent and its forks

- **GIVEN** a parent, its fork, and a fork of the fork that copied calls from both
- **WHEN** each session's lineage is read
- **THEN** the parent names the fork only, the fork names the parent and the fork of the fork, and the fork of the fork names the fork

#### Scenario: Another user's transcripts do not make a lineage

- **GIVEN** another user's two transcripts, one named for this session and holding a copy of a call in the other
- **WHEN** this session's lineage is read, and that user's own session's
- **THEN** neither names the other, since a link stands only between sessions one user owns, and a session with no retained process names none

#### Scenario: A fork's job names its immediate parent

- **GIVEN** a fork of a session cleared with `/clear`, and a fork of a fork that made no calls, each carrying the parent its job names
- **WHEN** their lineage is read
- **THEN** each names that parent, and the cleared session's call it copied is listed under the cleared session only

#### Scenario: A fork without its own evidence is read as the earlier session's

- **GIVEN** a fork with no job, as before Claude Code 2.1.301: a fork of a fork that made no calls, and a fork made after `/clear` whose copies name the process's first session
- **WHEN** their lineage is read
- **THEN** the first reads as the earlier session's fork, and the second names no parent and lists its copies of the cleared session's calls as its own

### Requirement: Alerts by agent session

`GET /api/alerts` SHALL accept `agent_session` with `host_id` and return only the alerts raised on that AI agent session's processes on that host, with the list's other filters still applied. A request with `agent_session` and no `host_id` SHALL be rejected, since a session id names a session only on its host.

#### Scenario: A session's alerts are listed

- **GIVEN** alerts on a session's processes, a monitor record on one of them, an alert on another session's process, and an alert on a process of the same session id on another host
- **WHEN** the alerts are listed with the host and the session
- **THEN** only the session's alerts on that host are returned
- **AND** asking for monitor records returns the monitor record instead

#### Scenario: A session filter without its host is rejected

- **GIVEN** a request for alerts with `agent_session` and no `host_id`
- **WHEN** it is made
- **THEN** it is rejected with `agent_session_requires_host_id` and the alerts are not read

### Requirement: Agent sessions list

`GET /api/agent-sessions` SHALL list AI agent sessions most recently active first, each with its agent, version, session id, host, project directory, first and last seen, and retained process and alert counts, filtered by host, session id, agent and the window it was active in, paged by an opaque keyset cursor with the filtered total. It SHALL require fleet-wide process read and reject a malformed cursor or an inverted window.

#### Scenario: Sessions are listed with what they did

- **GIVEN** agent sessions on two hosts, one with processes and alerts raised on them
- **WHEN** the sessions list is read
- **THEN** each session appears once, most recently active first, with its project directory, times, process count and alert count
- **AND** monitor records are not counted as alerts

#### Scenario: Filters narrow the list

- **GIVEN** sessions of different agents on different hosts at different times
- **WHEN** the list is read with a host, a host and session id, an agent, or a window
- **THEN** only matching sessions are returned, with their total

#### Scenario: The list pages by cursor

- **GIVEN** more sessions than the page limit
- **WHEN** the list is read page after page with each page's cursor
- **THEN** every session is returned exactly once when none changes between pages
- **AND** no session is returned twice when one becomes active between pages, since it moves ahead of the cursor
- **AND** a malformed cursor, or one missing any of its members, is rejected

### Requirement: Agent tool calls endpoint

`GET /api/hosts/{host_id}/agent-sessions/{session_id}/tool-calls` SHALL list the tool calls the session recorded, oldest first, each `tool_use_id` once per transcript owner at its first recording, read over the session's span widened by an hour on each side, and SHALL say when the list was cut. An unknown session SHALL be answered with `404 not_found`.

#### Scenario: A session's tool calls are listed once each

- **GIVEN** a session whose agent reported one call twice, around a restart, beside another session's call and another host's
- **WHEN** its tool calls are read
- **THEN** each of its calls is listed once, at its first recording, oldest first
- **AND** no other session's or host's call is listed

### Requirement: Alert chain descendants by story

When the pinned process carries a story root, the chain read SHALL fetch its candidate descendants in one indexed query, the story's processes and unstamped ones forked from the pinned process's fork to the window's end, and select descendants by the walk's own parent and lifetime rule, without the walk's descendant cap. A process without a story root, or a story past the candidate bound, SHALL be walked as before.

#### Scenario: The story read matches the walk

- **GIVEN** a pinned process whose descendants include a child forked before its creator's row was recorded, beside an unrelated process on the host
- **WHEN** its chain is read with and without a story root
- **THEN** both reads return the same descendants under the same parents
- **AND** the unstamped child is included and the unrelated process is not

#### Scenario: A chain past the walk's cap is read whole

- **GIVEN** a pinned process with more descendants than the walk's cap, in its story
- **WHEN** its chain is read
- **THEN** every descendant is returned and the read is not truncated
- **AND** the descendants cost one query whatever their depth

### Requirement: Inferred launch links

A window, session or story tree read SHALL link an `open -a <App>`, `open <path>.app` or `launchctl submit ... -- <program>` exec to the first process launchd started (not adopted) within 5 seconds of it that runs what it named, earliest ask first, each launched process once, for at most 50 launchers per read. A session or story read SHALL include a linked process with its descendants and report truncation when they are cut. A link SHALL NOT change parentage.

#### Scenario: A session read links and includes what its launchers started

- **GIVEN** an agent session whose tool calls ran `open -a TextEdit` and `launchctl submit -- /bin/sleep 61`, after which launchd started TextEdit, carrying the session's markers, and `/bin/sleep`, carrying none, each through an xpcproxy row
- **WHEN** the session is read
- **THEN** the response links the `open` to TextEdit and the `launchctl` to `/bin/sleep`
- **AND** `/bin/sleep` and what it started are in the forest, without the xpcproxy row, and TextEdit is listed once

#### Scenario: A launcher is matched to what it named

- **GIVEN** a launchd child that runs something else within the window, a process of the agent's own running the same program, the same program started by launchd before the ask, a daemon launchd adopted rather than started, a second `open -a TextEdit` before the app started, a later `open -a TextEdit` whose own launch follows it, and launchers that name a bundle id or a plist
- **WHEN** the tree is read
- **THEN** each launcher is linked to its own launch, and none of the other processes, nor the launchers naming nothing a path matches, are linked

#### Scenario: A window read links only what it holds

- **GIVEN** a window that holds a `launchctl submit` but ends before launchd started its program
- **WHEN** the window is read
- **THEN** no link is made for it and no row outside the window is added

### Requirement: Launches by label and bundle id

A tree read SHALL link a `launchctl kickstart <domain>/<label>` or `launchctl start <label>` exec to the program launchd started from the xpcproxy row naming that label, and an `open -b <bundle id>` exec to the launchd child whose code-signing identifier is the bundle id, ignoring case, within 15 seconds. A match SHALL run as the uid a `gui/` or `user/` domain names, or as the caller for `start` and `open -b`. An empty label or bundle id SHALL name nothing.

#### Scenario: A label or bundle id names its launch exactly

- **GIVEN** `kickstart -k gui/501/<label>`, `start <label>` and `open -b com.apple.TextEdit` execs
- **WHEN** each is matched against launchd children
- **THEN** the xpcproxy row naming the label, or the app signed `com.apple.textedit`, matches, and another label, another identifier or an unsigned app does not

#### Scenario: A session read links a kickstart, a start and an open by bundle id

- **GIVEN** a session that ran `kickstart -k` on a job launchd restarted 8.3 s later, `start` on another job, and `open -b com.apple.TextEdit`, among another job's start, the same label started past the window, and an app signed with another identifier
- **WHEN** the session's tree is read
- **AND** the same label started first in another user's session, and a `start ""`
- **THEN** each command is linked to its own program or app, the job's xpcproxy row is not listed, and no decoy is linked

### Requirement: Launches by job definition

A tree read SHALL link a `launchctl load` or `bootstrap` exec carrying `launchd_jobs` to the program launchd started from each xpcproxy row naming one of their labels within 15 seconds, one launch per job. A bootstrap SHALL be held to the uid its `gui/` or `user/` domain names, and a load to its caller's unless the caller is root.

#### Scenario: A load links each job it defined

- **GIVEN** a session's `launchctl load` of two plists defining two labels, and launchd starting an unrelated job between the two it loaded
- **WHEN** the session's tree is read
- **THEN** the load is linked to both jobs' programs, not to launchd's xpcproxy rows, and not to the unrelated job

### Requirement: Process activity rollup

`GET /api/hosts/{host_id}/activity` SHALL total and list the files, network destinations and DNS names that a process, its subtree, an AI agent session or a story touched, naming the process behind each entry. It SHALL match events to processes by exact generation, count a coalesced event as the occurrences it stands for, cap each list and report the cut, and report how far the scope reached. It SHALL require process read.

#### Scenario: A scope's activity is totalled and listed by process

- **GIVEN** two processes that opened, renamed, connected and resolved, including a coalesced connection standing for three, an event of a later process that reused one's pid, and an event with no generation
- **WHEN** their activity is read
- **THEN** each class's event total and distinct count match a count by hand, the coalesced connection counting three
- **AND** each list names the process behind each entry, and leaves out the reused pid's event and the event with no generation
- **AND** a list read with a smaller cap reports that it was cut, while the totals stay the scope's

#### Scenario: A process or its subtree is totalled

- **GIVEN** a process with an ancestor and two children, one of which carries no generation
- **WHEN** the activity is read for the process alone, and then for its subtree
- **THEN** the first reads only the process, and the second the process and its child with a generation, never the ancestor
- **AND** the scope report counts the child without a generation as unmatched

#### Scenario: A session or story is totalled

- **GIVEN** an AI agent session, or a story, read by its index
- **WHEN** its activity is read
- **THEN** every process of it that carries a generation is matched, and the scope report says when the read was truncated

#### Scenario: A malformed activity scope is rejected

- **GIVEN** a request with no scope, a scope without its target, or the timeline's `chain` scope
- **WHEN** it is made
- **THEN** it is rejected with `invalid_scope`

### Requirement: Process search by lineage

`GET /api/search/processes` SHALL accept `agent`, `agent_session` (with `host_id`), `story_root`, `ancestor` and `args`, composing with the other filters. `ancestor` SHALL keep each descendant once, by creator and re-exec edges, up to 900 levels down, not the process itself. `args` SHALL match a substring of one argument, case-sensitively. A non-positive `story_root` or `ancestor` SHALL be rejected with `400 invalid_process`.

#### Scenario: A search keeps a session, a story or a process's descendants

- **GIVEN** a story holding a Claude Code tool call's shell, its curl and the curl's re-exec, beside other sessions' curls and one no agent ran
- **WHEN** the search filters by `agent=claude-code` and a path of `curl`, by the session, by the story's root, or by `ancestor` of the shell
- **THEN** it returns the curls of every Claude Code session, the session's processes, the story's processes, or the curl and its re-exec
- **AND** a `story_root` of `0` is rejected with `invalid_process`

#### Scenario: Arguments are matched one at a time

- **GIVEN** a process whose arguments are `curl`, `-s` and `https://example.com`
- **WHEN** the search filters by `args`
- **THEN** `example.com` matches it, while `EXAMPLE.COM` and `curl -s`, which spans two arguments, do not

#### Scenario: A process's descendants are each listed once

- **GIVEN** a shell that re-execs three times, each generation forking a child, and an ancestor whose story was not seen above a child whose story was
- **WHEN** the search filters by `ancestor` of either
- **THEN** each generation and child is listed once, and the child with a story is listed under the ancestor without one

#### Scenario: A deep chain is walked to its bound

- **GIVEN** a fork chain 1,100 processes deep
- **WHEN** the search filters by `ancestor` of its top
- **THEN** it counts the 900 descendants within the bound rather than failing

### Requirement: Process forest links by creator

The process forest SHALL link a process to its creator row, and SHALL return it as a root when that row is not in the result. A process without one SHALL link to the row in the result with its exact `(ppid, parent_pidversion)` that was alive at its fork, else to the newest row holding its `ppid`. A pinned process's ancestor walk SHALL follow the same evidence, ending where a creator is not stored. A chain's descendants SHALL exclude a child whose creator row is another process.

#### Scenario: The creator row beats a reused pid

- **GIVEN** a result holding a process's creator and a later process that reused the creator's pid
- **WHEN** the forest is built
- **THEN** the process hangs under its creator

#### Scenario: A creator outside the result leaves the process a root

- **GIVEN** a process whose creator row is not in the result, and a later process holding the creator's pid that is
- **WHEN** the forest is built
- **THEN** the process is a root

#### Scenario: An exact generation beats a reused pid

- **GIVEN** a process with no creator row whose `(ppid, parent_pidversion)` names a row in the result, and a later process holding that pid
- **WHEN** the forest is built
- **THEN** the process hangs under the row of that generation
- **AND** a row holding that identity that forked after the process, or had exited before it, is not its parent

#### Scenario: The ancestor walk follows creators through a double fork

- **GIVEN** a daemon whose creator exited before launchd adopted it, and a later process that reused the creator's pid
- **WHEN** the forest or the chain is read pinning the daemon
- **THEN** its ancestors are its creator and the creator's own creator
- **AND** a creator that retention removed ends the chain

#### Scenario: A chain excludes a child of another process

- **GIVEN** a process and a child whose `ppid` matches it inside its lifetime but whose creator row is another process
- **WHEN** the process's chain is read
- **THEN** the child is not among its descendants

### Requirement: Responsible process links

A story or session tree read SHALL link each process it holds that is responsible for itself to every process launchd started that names it, by pid and generation, as responsible within its lifetime, and SHALL add each such process with its descendants as an extra root. A window read SHALL link only what it holds. A read SHALL report a cut when an app has more such processes than are read.

#### Scenario: A session read links and includes the services of its apps

- **GIVEN** a session holding TextEdit, launched by launchd and responsible for itself, and two XPC services launchd started that name TextEdit's pid and generation
- **AND** decoys: a service naming TextEdit's pid under another generation, one naming another app, one started after TextEdit exited, and one naming the SSH login a shell command ran under
- **WHEN** the session's tree is read
- **THEN** the two services are linked to TextEdit as `responsible_process` and added with what they started, and no decoy is linked or added

### Requirement: Session tree names its agent

A session's tree read SHALL return the session's agent processes in `agents`: the creator of a root whose pid is the session's agent pid. Its `roots`, counts and truncation SHALL be those of the session's own processes. Other reads SHALL return no `agents`.

#### Scenario: A session's tree names the agent that ran its commands

- **GIVEN** a session whose agent created two of its roots, and a root another process created
- **WHEN** the session's tree is read
- **THEN** `agents` holds the agent's process, and the three roots stay roots
- **AND** a story read returns no `agents`, and a failed session lookup fails the read

### Requirement: Host timeline scopes

The host timeline SHALL accept `scope=chain&pin=<process id>`, `scope=session&session=<id>` and `scope=story&root=<process id>`, read the scope's processes with the process tree's reads, filter events to their generations, and report whether it applied, its process count, how many carry no generation, and truncation. A scope with no generation SHALL return the whole host's stream, reported not applied. A scope without a target, or with `chain=`, SHALL be rejected.

#### Scenario: The alert chain is resolved by the server

- **GIVEN** an alerted process with ancestors and descendants, and another process that reused one of their pids
- **WHEN** the timeline is read with `scope=chain` pinning the alerted process
- **THEN** only events of the chain's generations are returned
- **AND** the report counts the chain's processes and those without a generation

#### Scenario: A session or story scopes the timeline

- **GIVEN** an agent session's processes among the host's other activity
- **WHEN** the timeline is read with `scope=session`, or `scope=story` naming the story's root
- **THEN** only events of that scope's generations are returned

#### Scenario: A scope with nothing to match shows the whole host

- **GIVEN** a scope naming no stored process, or one whose processes carry no generation
- **WHEN** the timeline is read with it
- **THEN** the whole host's stream is returned with the scope reported not applied and its process count

#### Scenario: A malformed timeline scope is rejected

- **GIVEN** a scope without a usable pin, session or root, or one combined with `chain=`
- **WHEN** the timeline is read
- **THEN** the response is a client error with `invalid_scope`

### Requirement: Story and session tree reads

The process-forest endpoint SHALL accept `scope=story&root=<process id>` and `scope=session&session=<id>`, returning the host's processes of that story or AI agent session by an indexed read with no time window, oldest first, bounded by the row limit with the window read's truncation and count metadata. A story or session scope without a valid root or session SHALL be rejected as a client error.

#### Scenario: A session is read whole

- **GIVEN** an agent session whose processes outnumber the chain read's descendant cap
- **WHEN** the forest is read with `scope=session` and a limit above the session's size
- **THEN** every process of the session is returned, linked under its creator, and the read is not truncated
- **AND** the read costs one query whatever the depth of the tree

#### Scenario: A scope beyond the limit reports truncation

- **GIVEN** a story with more processes than the row limit
- **WHEN** the forest is read with `scope=story`
- **THEN** the oldest processes up to the limit are returned
- **AND** the read is reported as truncated with the story's count

#### Scenario: A scope reads only its host

- **GIVEN** the same session id or story root id on two hosts
- **WHEN** one host's forest is read with that scope
- **THEN** only that host's processes are returned

#### Scenario: A scope without its target is rejected

- **GIVEN** `scope=story` without a positive `root`, or `scope=session` without a `session` of at most 256 bytes
- **WHEN** the forest is read
- **THEN** the response is a client error with `invalid_scope`
