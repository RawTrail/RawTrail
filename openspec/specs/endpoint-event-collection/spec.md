# Endpoint Event Collection Specification

## Purpose

Endpoint event collection is the on-device telemetry source for RawTrail. It captures the security-relevant operating-system activity that downstream detection rules, the process-tree UI, and incident-response workflows depend on: process lifecycle (exec / fork / exit / open), outbound and inbound network flows attributed to the originating process, and DNS queries with their resolved answers. Without this layer there is no signal: every other component in the system is consuming or relaying what this capability emits.

The behavior described here is the contract agents and the server depend on. It defines what events appear, what fields each event carries, how events are uniquely identified, and how the host that produced them is identified, so that the ingest path, the storage schema, and the React UI can reason about the wire format without reading device source code.

## Requirements

### Requirement: Process lifecycle event capture

The system SHALL emit a `fork` event when a monitored process forks, an `exec` event when a process replaces its image, and an `exit` event when a process exits. Each event MUST carry the originating PID and any additional fields documented for that event type. The `exec` event SHALL additionally carry the process's own kernel PID generation and the `fork` event SHALL carry the child process's kernel PID generation (`pidversion`, read from the respective process's audit token) when it is available, so the server can disambiguate reused PIDs by identity rather than by time. The `pidversion` field is optional: when the audit token is unavailable the event is still emitted without it.

The `exec` event SHALL carry `cdhash`, the code-directory hash of the new image, when the process runs under Apple's Hardened Runtime AND the kernel reported a hash for it. It SHALL omit the field otherwise, in both of the cases that reach that outcome: a process not running under the Hardened Runtime, and a hardened process whose reported hash is all zeros.

Both omissions are deliberate. The kernel maps pages lazily on a non-hardened process and does not re-verify them after load, so the hash reported at exec is not a reliable identity for the bytes that will eventually execute. An all-zero hash is the kernel saying it has none, and emitting it would let a rule whose identifier is forty zeros match by coincidence. A value that cannot be relied on is worse than an absent one.

The field is what lets an operator exclude a code-signed parent from a `suspicious_exec` finding by its non-spoofable code identity instead of a path glob an attacker who can write to a world-writable directory could land inside, so its absence where a hash does exist is a loss of that defence and not a cosmetic gap.

#### Scenario: A user runs a shell command

- **GIVEN** the endpoint event capture is running
- **WHEN** a user launches `/bin/ls` from a shell
- **THEN** the system emits an `exec` event whose payload includes the new image path, the argument vector, the parent PID, the effective UID and GID, and the code-signing identity (team ID, signing ID, platform-binary flag) when the binary is signed
- **AND** the payload includes the process's `pidversion` when its audit token is available
- **AND** the system later emits an `exit` event for the same PID with the process exit status

#### Scenario: A daemon forks a worker

- **GIVEN** the endpoint event capture is running
- **WHEN** a process calls `fork(2)` without a subsequent exec
- **THEN** the system emits a `fork` event whose payload identifies the parent PID and the child PID
- **AND** the payload includes the child process's `pidversion` when its audit token is available

#### Scenario: The exec event carries cdhash only when the kernel reported one

- **GIVEN** the endpoint event capture is running
- **WHEN** a process execs a binary that runs under Apple's Hardened Runtime and the kernel reports a hash for it
- **THEN** the `exec` event payload carries `cdhash`
- **AND** an exec of a binary that does not use the Hardened Runtime omits `cdhash`
- **AND** an exec whose reported hash is all zeros omits `cdhash` rather than carrying forty zeros
- **AND** the event is otherwise well-formed in every case

### Requirement: Launch-item registration event capture

The system SHALL emit a `btm_launch_item_add` event when launchd registers a launch item (a LaunchDaemon, LaunchAgent, or login item) via Background Task Management. The payload MUST carry the item type, the launch item path, the registered executable path when available, the MDM-managed flag, and the code-signing identity of the REGISTERED EXECUTABLE (`executable_code_signing`: team ID, signing ID, platform-binary flag) evaluated out-of-band, because the event provides code-signing for the instigator process but not for the to-be-launched executable.

Background Task Management reports an item registered through `SMAppService` relative to the app that registered it, and reports that app separately. The payload MUST carry that app as `app_url`, and the launch item path MUST be uploaded resolved against it, as an absolute `file://` URL. A login item, and an app added to the user's login items, have no registered executable path: the registered executable is the app bundle the item names (a helper inside the registering app for `item_type=login_item`, the app itself for `item_type=app`), and `executable_code_signing` MUST be that bundle's.

#### Scenario: A LaunchDaemon is registered via Background Task Management

- **GIVEN** the endpoint event capture is running
- **WHEN** launchd registers a system LaunchDaemon (for example via `launchctl bootstrap`)
- **THEN** the system emits a `btm_launch_item_add` event whose payload includes `item_type=daemon`, the launch item path, the registered executable path, the MDM-managed flag, and the registered executable's code-signing identity

#### Scenario: A login item is registered through SMAppService

- **GIVEN** the endpoint event capture is running
- **WHEN** an app registers a helper in its `Contents/Library/LoginItems/` as a login item
- **THEN** the uploaded `btm_launch_item_add` event has `item_type=login_item` and the registering app as `app_url`
- **AND** its launch item path is the helper bundle's absolute `file://` URL
- **AND** its `executable_code_signing` is the helper bundle's code-signing identity

#### Scenario: An app is added to the user's login items

- **GIVEN** the endpoint event capture is running
- **WHEN** an app is added to the user's login items, by itself through `SMAppService` or through the legacy login-items list
- **THEN** the uploaded `btm_launch_item_add` event has `item_type=app` and the app bundle's absolute `file://` URL as its launch item path
- **AND** its `executable_code_signing` is the app bundle's code-signing identity

### Requirement: Sensitive-path file-modification capture

The system SHALL emit a write-mode `open` event when a process creates or writes a file under the sensitive target set, carrying the writing process PID, the file path, and write-mode access flags. The sensitive target set is the built-in paths (`/etc/sudoers` and anything under `/etc/sudoers.d/`) together with the watched-path set the server last pushed. The system SHALL NOT forward a broad stream of file opens: collection is scoped at the source to the sensitive target set via a dedicated Endpoint Security client with inverted target-path muting, kept separate from the process-authorization client so the scoping never affects exec authorization (ADR-0008). Writes to paths outside the sensitive set MUST NOT be collected.

The system SHALL additionally emit a `file_rename` event when a process renames a file into, within, or out of that same sensitive set, carrying the renaming process PID, the source path, and the destination path. Both paths are required: a rename is the only operation in this set that makes a file become sudo policy without any write to the destination, and the source is what distinguishes a file promoted from a scratch path elsewhere from one already inside the watched directory.

Renames SHALL be collected under the same inverted target-path muting as creations and writes, which matches a rename when EITHER of its paths falls in the sensitive set.

#### Scenario: A write to a sensitive path is captured

- **GIVEN** the extension is running with the sensitive-path file-modification client active
- **WHEN** a process writes to `/etc/sudoers` (or a direct child of `/etc/sudoers.d/`)
- **THEN** a write-mode `open` event is emitted carrying the writing process PID, the file path, and the write-mode access flags
- **AND** the event reaches the server and is available to the detection pipeline

#### Scenario: A rename event carries both of its paths

- **GIVEN** a rename touching the sensitive set has been observed
- **WHEN** the extension serializes it
- **THEN** the `file_rename` event carries the renaming process PID, the source path, and the destination path
- **AND** the destination is carried under the same field name every other file event uses for its target, so one detection can read both

Note on verification: this scenario pins the event's SHAPE, which is what the extension's unit tests can reach. That the ESF client is subscribed and that `handleRename` reads both halves of the rename union are exercised at the system / VM layer per `docs/testing-strategy.md`, because `FileTamperSubscriber` imports EndpointSecurity and is outside the unit-testable target.

### Requirement: Outbound socket flow capture

The system SHALL emit a `network_connect` event for every outbound socket flow seen by the network filter, attributing the flow to the source process. Inbound flows SHALL be tagged with `direction = inbound`. The system MUST NOT block flows on the basis of capture; capture is observation-only. The `network_connect` payload SHALL additionally carry the source process's kernel PID generation (`pidversion`, read from the flow's audit token) when it is available, so the server can correlate the flow to the exact process generation. The `pidversion` field is optional: when the flow carries no usable audit token the event is still emitted without it.

#### Scenario: A process opens an outbound TCP connection

- **GIVEN** the network filter is enabled
- **WHEN** a process initiates an outbound TCP connection to a remote endpoint
- **THEN** the system emits a `network_connect` event whose payload identifies the source PID, the source binary path, the effective UID, the protocol (`tcp` or `udp`), the direction (`outbound`), the remote address, the remote port, the local address, the local port, and (when the system can derive it from the flow) the remote hostname
- **AND** the payload includes the source process's `pidversion` when the flow's audit token is available
- **AND** the flow is allowed to proceed unmodified

### Requirement: DNS query capture

When DNS proxying is enabled, the system SHALL emit a `dns_query` event for each DNS query seen by the DNS proxy and a follow-on `dns_query` event carrying the resolved addresses when the upstream resolver replies. DNS proxying is opt-in (see the host-app extension manager capability), so a host with the DNS proxy disabled emits no `dns_query` events at all and that absence is not a contract violation. The `dns_query` payload SHALL additionally carry the querying process's kernel PID generation (`pidversion`, read from the flow's audit token) when it is available. The `pidversion` field is optional: when the flow carries no usable audit token the event is still emitted without it. Capture failures MUST NOT prevent the query or its response from being forwarded to the originally-intended resolver.

Because an enabled DNS proxy is the sole resolver for every claimed flow, forwarding itself MUST be resilient: each upstream forward SHALL be bounded by a deadline, and on forward failure or deadline expiry for a query that no active enforcement policy blocks the proxy SHALL fail open by releasing the flow cleanly rather than leaving the client's resolution hung. The proxy MUST NOT pin a flow indefinitely waiting on an upstream that never answers. Telemetry remains strictly best-effort and never gates forwarding or the fail-open path. Health-driven recovery from sustained forwarding failure is specified by the network-response capability.

#### Scenario: An application resolves a hostname over UDP

- **GIVEN** DNS proxying is enabled
- **WHEN** an application sends a UDP DNS query for a hostname
- **THEN** the system forwards the query unchanged to the originally-intended resolver
- **AND** the system emits a `dns_query` event identifying the source PID, source path, effective UID, query name, query type, and protocol (`udp`)
- **AND** the payload includes the source process's `pidversion` when the flow's audit token is available
- **AND** when the resolver replies with one or more addresses the system emits a follow-on `dns_query` event carrying the resolved addresses in `response_addresses`

#### Scenario: A DNS query that cannot be parsed is still forwarded

- **GIVEN** DNS proxying is enabled
- **WHEN** an application sends a DNS query whose payload the proxy cannot parse for telemetry
- **THEN** the system forwards the query to the originally-intended resolver
- **AND** the system does not emit a `dns_query` event for the unparsed payload

#### Scenario: An upstream that never replies does not hang resolution forever

- **GIVEN** DNS proxying is enabled and no active enforcement policy blocks the query
- **WHEN** an application sends a DNS query and the upstream resolver does not answer within the forward deadline
- **THEN** the proxy releases the flow rather than holding it open indefinitely
- **AND** the client's resolution is free to proceed (retry or roll over to another resolver) instead of being pinned by the proxy
- **AND** any telemetry for the query is best-effort and its absence is not a contract violation

### Requirement: Canonical event envelope

Every event the system emits SHALL be serialized as a JSON envelope with the fields `event_id`, `host_id`, `timestamp_ns`, `event_type`, and `payload`. `event_id` MUST be a UUID unique to that event, `host_id` MUST identify the device that produced the event and MUST be stable across reboots of that device, `timestamp_ns` MUST be nanoseconds since the Unix epoch, and `event_type` MUST be one of the values the published event schema (`schema/events.json`) documents for it, and every published copy of that list, such as the ingest request schema in the OpenAPI document, MUST name the same values. The change from the prior requirement is that the list is the schema's rather than one written out here, which had fallen behind it; `application_control_would_block` joins the schema with Detect mode.

`timestamp_ns` SHALL record when the KERNEL observed the event, not when the agent finished handling it. For every event derived from an Endpoint Security message the system SHALL take the stamp from that message's own event time. Sampling the wall clock at serialization instead records handler latency, and that latency is not small: an exec is serialized after the handler's synchronous hash and code-signing work, measured at 701ms after the true exec on a loaded host. Because the server correlates a network flow to the process that produced it by comparing these stamps, a late process stamp can place a process AFTER the flow it produced, and the correlation then finds nothing and reports nothing. It also makes every time-correlated detection load-dependent, so fast scripted chains are missed while slow interactive ones are caught.

An event that does not originate from a kernel message, such as a state reconciliation or a boot-time process snapshot, SHALL carry the time it was produced, because there is no kernel instant for it to report.

#### Scenario: An event envelope is well-formed

- **GIVEN** any captured event
- **WHEN** the system serializes the event
- **THEN** the resulting bytes parse as a JSON object containing `event_id`, `host_id`, `timestamp_ns`, `event_type`, and `payload`
- **AND** `event_type` matches one of the documented enum values
- **AND** the payload conforms to the schema for that event type

#### Scenario: Published copies of the event type list agree

- **GIVEN** the event schema's `event_type` values
- **WHEN** an operator reads the ingest request schema in the OpenAPI document
- **THEN** it lists the same values

#### Scenario: Events from the same device share a host_id

- **GIVEN** an enrolled device producing events
- **WHEN** the device emits events from any source (process, network, DNS)
- **THEN** every emitted event carries the same `host_id` value
- **AND** that value persists across reboots of the device

#### Scenario: A kernel event is stamped with the kernel's own event time

- **GIVEN** a kernel message reporting an event that occurred at a known instant
- **WHEN** the system serializes an event from that message, after doing work that takes measurable time
- **THEN** the envelope's `timestamp_ns` is the instant the kernel reported, not the instant serialization ran
- **AND** the value is nanoseconds since the Unix epoch, on the same clock the server compares other events against

#### Scenario: An event with no kernel message behind it is stamped when produced

- **GIVEN** an event that reports state read by the agent rather than a kernel event, such as a reconciliation or a boot-time snapshot
- **WHEN** the system serializes it
- **THEN** the envelope carries the time the event was produced, because no kernel instant exists for it

### Requirement: Reconciliation events are tagged

The system SHALL distinguish synthesized reconciliation events from kernel-observed events. Synthetic exit events emitted to close out processes whose kernel exit notification was missed SHALL carry `exit_reason = host_reconciled`, synthetic exec events emitted at startup to materialize processes that already existed before subscription SHALL carry `snapshot = true`, and liveness pings for those snapshot-originated processes SHALL be emitted as a distinct event type `snapshot_heartbeat` carrying only the process identifier in its payload.

#### Scenario: Agent fills a missing exit event

- **GIVEN** a process disappeared from the kernel without producing an exit event
- **WHEN** the reconciliation pass detects the absent process
- **THEN** the system emits an `exit` event for the missing PID with `exit_reason = host_reconciled`

#### Scenario: Extension restarts and rebuilds the live process set

- **GIVEN** the system extension has just started
- **WHEN** the system enumerates processes that already existed before subscription began
- **THEN** the system emits one `exec` event per such process with `snapshot = true`
- **AND** detection rules ignore those events because they describe historical state

#### Scenario: Snapshot-originated process is still alive on a later pass

- **GIVEN** a snapshot-originated process that the kernel still reports as live
- **WHEN** the reconciliation pass probes the process
- **THEN** the system emits a `snapshot_heartbeat` event whose payload identifies the process
- **AND** detection rules ignore those events because they describe liveness rather than activity

### Requirement: Capture is non-fatal on individual event errors

The system SHALL continue capturing subsequent events when serialization, attribution, or upstream forwarding of a single event fails. A single malformed event MUST NOT take the capture pipeline offline.

#### Scenario: One event fails to serialize

- **GIVEN** the capture pipeline is running
- **WHEN** a single event cannot be serialized into the canonical envelope
- **THEN** the system drops that one event
- **AND** the system continues capturing and emitting subsequent events

### Requirement: Serialized events declare their platform

The macOS system extension serializers SHALL stamp the platform that produced the event on every event envelope they emit. Because this extension runs only on macOS, the stamped value SHALL be `darwin`, matching the server's canonical platform constant. The field is part of the platform-aware event contract so the server can scope detection rules by platform and surface it in the host inventory.

#### Scenario: An ESF event envelope carries the darwin platform

- **GIVEN** the endpoint-security serializer encodes an event envelope
- **WHEN** the encoded JSON is inspected
- **THEN** its `platform` field is `darwin`

### Requirement: Event payload schema is selected by event type

The published event schema SHALL select which payload definition applies from the envelope's `event_type`, and SHALL NOT select it by requiring the payload to match exactly one definition. Payload definitions overlap by construction: `snapshot_heartbeat_payload` requires only `pid`, so every payload carrying a `pid` satisfies it, and `file_truncate_payload` and `file_delete_payload` are identical in both their required and their declared fields. A schema that selects by matching exactly one definition therefore refuses envelopes the system emits and accepts, which is the opposite of what the document is for.

Every value of the documented `event_type` enum SHALL have exactly one selection clause, and that clause SHALL name a payload definition that exists. An event type without a clause leaves its payload wholly unconstrained while the document still appears to describe it.

The schema SHALL validate the envelopes the system's own emitters produce. The document is mirrored by hand in several emitters and is cited across the agent, the extension and the server as the wire contract, so an emitter that drifts from it MUST be observable rather than silent.

The schema constrains each payload's required fields and their types. It does NOT forbid fields beyond those it declares, because the ingest path accepts them; a payload that carries its own type's required fields plus additional keys is therefore accepted.

#### Scenario: Each documented event type validates

- **GIVEN** the published event schema
- **WHEN** an envelope is validated for each value of the documented `event_type` enum, carrying that type's documented payload
- **THEN** every one of them validates
- **AND** none is refused for matching more than one payload definition

#### Scenario: A mismatched payload is rejected

- **GIVEN** the published event schema
- **WHEN** an envelope carries a payload that does not satisfy the definition its own `event_type` selects
- **THEN** validation fails, naming the field that is missing or ill-typed

#### Scenario: Emitted envelopes validate against the document

- **GIVEN** the envelopes an emitter in this repository produces for its shipped scenarios
- **WHEN** each is validated against the published event schema
- **THEN** every envelope validates, including its `event_id` format

#### Scenario: A payload carrying an undeclared field is accepted

- **GIVEN** the published event schema
- **WHEN** an envelope carries its own event type's required fields plus a field the schema does not declare
- **THEN** it validates, because the ingest path accepts such a payload and the document must not be stricter than what the system accepts

#### Scenario: Every event type has a discriminator clause

- **GIVEN** the published event schema
- **WHEN** its selection clauses are compared against the documented `event_type` enum
- **THEN** each enum value has exactly one clause, and no clause names a type outside the enum
- **AND** each clause names a payload definition the document defines

### Requirement: Destruction of a sensitive file is captured

The system SHALL emit a `file_truncate` event when a process discards the contents of a file in the sensitive target set, and a `file_delete` event when a process removes one, each carrying the acting process PID and the path.

Truncation SHALL be captured however it is performed. `truncate(2)` and `ftruncate(2)` are one kernel path and an `open(2)` carrying `O_TRUNC` is another, and only the second is what a shell redirect uses, so capturing either alone leaves the common case invisible.

An open that does NOT discard contents SHALL NOT be emitted. The sensitive paths are read routinely, since every `sudo` invocation reads the policy, so reporting those reads would turn a destruction signal into a stream of ordinary privilege checks. The filter belongs in the extension rather than in a rule, because what is being avoided is what reaches the wire at all.

Note on verification: the scenarios below pin the event SHAPES, which is what this repository's tests can reach. That the ESF client is subscribed to the three event types, that an `O_TRUNC` open is told from a routine read, and that a truncate syscall and a shell redirect both arrive are exercised at the system / VM layer per `docs/testing-strategy.md`, because `FileTamperSubscriber` imports EndpointSecurity and is outside the unit-testable target.

#### Scenario: An emptied file is reported as a truncation

- **GIVEN** a process has discarded the contents of a file in the sensitive set
- **WHEN** the extension serializes the event
- **THEN** a `file_truncate` event carries the acting process PID and the path
- **AND** the same shape is produced whether the contents were discarded by a truncate syscall or by an open carrying `O_TRUNC`

#### Scenario: A removed file is reported as a deletion

- **GIVEN** a process has removed a file in the sensitive set
- **WHEN** the extension serializes the event
- **THEN** a `file_delete` event carries the acting process PID and the path
- **AND** it is distinguishable from a truncation, because an emptied file still exists and a removed one does not

### Requirement: The watched-path set is pushed by the server

The system extension SHALL accept a watched-path set from the server, delivered by the agent, and SHALL make it part of the sensitive target set on the running file-tamper client without a restart. A set is a `version`, an optional `epoch` (the set's server update time in Unix microseconds), and a list of `paths` entries, each an absolute `path` and a `match` of `literal` (exactly that file) or `prefix` (every path starting with it).

The extension SHALL apply a set only when it is ahead of the last set it accepted, ordered by `epoch` and then by `version`, and SHALL otherwise leave the active and persisted sets unchanged. Commands can reach the host out of order, so without this an older set delivered late would replace a newer one. The server forces each set's `epoch` past the one in its database, so `epoch` orders every set it issues across a step back in the database clock, and across a server database restore that sends `version` backwards once the clock is past the epochs issued before the restore; a restore whose database clock is still behind them delays new sets until it passes them, as ordering on either axis did; `version` breaks a tie, which is what orders sets from a server that sends no `epoch`; every server that pushes this set sends one. A set issued before a restore, still on its way to the host when a set is saved since, is therefore older and is not applied, even though its `version` is higher. This is the rule application control uses for its policy snapshots, and the two SHALL keep one rule.

The built-in paths SHALL stay watched whatever set is pushed: a pushed set adds to them and cannot remove them, because the shipped sudoers detections depend on them. An empty pushed set SHALL therefore leave exactly the built-in paths watched.

Replacing the set SHALL NOT stop observing a path that both the previous and the new set cover. A path under a firmlinked root (`/etc`, `/tmp`, `/var`) SHALL be watched in both its `/private` and its root-linked spelling, since Endpoint Security reports the resolved form.

The extension SHALL persist a set before applying it and SHALL start from the persisted set, so a restarted extension watches the pushed paths before the agent next delivers a set. A set that cannot be persisted SHALL NOT be applied, so the running set and the one a restart loads never differ; a later delivery of the same set tries again.

An entry with a `match` the extension does not know SHALL be skipped while the rest of the set is applied, so an older extension keeps watching what it understands when a newer server adds a kind of entry. So SHALL any entry the server's rules for an entry refuse, which the extension holds again rather than relying on the server to have held them, because what reaches the kernel is a C string: a path that is not absolute, is longer than 1023 bytes in its `/private` spelling (`PATH_MAX` less the C string's terminating NUL), contains an ASCII control character (NUL included), or has an empty, `.` or `..` segment; a `literal` ending in `/`; and a `prefix` not ending in `/` or not lying below a top-level directory (judged through `/private` for `/etc`, `/tmp` and `/var`). A top-level prefix, however it is spelled, would put every write under that tree on the wire. A payload that is not a watched-path document at all SHALL leave the watched set, and the persisted one, unchanged.

A pushed path that the client cannot mute SHALL be logged rather than end the extension, since the set persists and ending the extension would restart it into the same failure. When any mute in an update fails, the running client SHALL NOT unmute anything that update drops, and the failed path is attempted again by the next update. That retention holds for the running process only: a restarted extension watches the persisted set.

Note on verification: decoding, the combination with the built-in paths, the mute difference between two sets, and persistence are pinned by the extension's unit tests. That the running client observes an added path and stops observing a dropped one is exercised at the system / VM layer per `docs/testing-strategy.md`, because `FileTamperSubscriber` imports EndpointSecurity and is outside the unit-testable target.

#### Scenario: A pushed set is applied without a restart

- **GIVEN** the file-tamper client is running and watching a pushed set
- **WHEN** a new set arrives that adds one path and drops another
- **THEN** the client starts observing the added path and stops observing the dropped one
- **AND** paths in both sets are not unmuted at any point, and the extension does not restart

#### Scenario: A set that cannot be persisted is not applied

- **GIVEN** the extension cannot write its persisted set
- **WHEN** a set that supersedes the current one arrives
- **THEN** the set is not applied, the current set stays in force, and a later delivery of the same set is still treated as new

#### Scenario: An older set delivered late does not replace a newer one

- **GIVEN** the extension has accepted a set at a given version and epoch
- **WHEN** a set arrives that is behind on both version and epoch, or is the same set again
- **THEN** the active set and the persisted set are unchanged
- **AND** a set whose version is behind but whose epoch is ahead, as after a server database restore, is applied

#### Scenario: A pre-restore set is refused

- **GIVEN** the extension has accepted a set saved after a server database restore, whose version is lower than before the restore
- **WHEN** a set issued before the restore arrives, with a higher version and an earlier epoch
- **THEN** the active set and the persisted set are unchanged

#### Scenario: A prefix at the top of the filesystem is not watched

- **GIVEN** a pushed set containing a `prefix` entry for `/`, a top-level directory such as `/Users/`, `/private/etc/`, or a path that only reads as deeper through a NUL or `..` segments
- **WHEN** the extension decodes it
- **THEN** those entries are skipped and counted, and the remaining entries are applied

#### Scenario: The built-in paths stay watched whatever is pushed

- **GIVEN** any pushed set, including an empty one
- **WHEN** the client applies it
- **THEN** `/etc/sudoers` and `/etc/sudoers.d/` are watched in both spellings, exactly as before the set was configurable

#### Scenario: A pushed set survives an extension restart

- **GIVEN** a set has been pushed and applied
- **WHEN** the extension restarts
- **THEN** it loads the persisted set and watches those paths from its first event, before the agent delivers a set again

#### Scenario: An entry the extension does not understand is skipped

- **GIVEN** a pushed set containing an entry with an unknown `match`, or a path that is not absolute
- **WHEN** the extension decodes it
- **THEN** that entry is skipped and counted, and the remaining entries are applied

#### Scenario: A malformed push leaves the watched set unchanged

- **GIVEN** a watched set is active
- **WHEN** a push arrives whose payload is not a watched-path document
- **THEN** the active set and the persisted set are unchanged

### Requirement: Browser credential reads are reported

The security extension SHALL watch the credential files of Chrome, Brave, Edge, Arc, Vivaldi and Firefox in every profile of every home it reads from the directory service: for a Chromium browser, `Local State` and each profile's `Login Data`, `Login Data For Account`, `Web Data`, `Cookies` and `Network/Cookies`; for Firefox, each profile's `logins.json`, `key4.db` and `cookies.sqlite`. A Chromium profile is the `Default` directory or one named `Profile`, a space and a number written without leading zeros, as in `Profile 1`; a Firefox profile is a directory under `Profiles/` whose name holds a dot and does not start with one. It SHALL watch each file as a literal path and re-read the accounts and profiles at least every five minutes. When one browser directory holds more than 50 profiles, it SHALL watch that directory as a prefix instead of each file, report only opens of the credential files named here, and log that the directory held more. It SHALL report an open of a watched file as an `open` event carrying the open's access mode, unless the opening process is signed by the team that signs the browser owning the file.

#### Scenario: Every profile's credential files are watched

- **GIVEN** a home with Chrome profiles `Default` and `Profile 1`, a `System Profile` and a `Crashpad` directory beside them, and one Firefox profile
- **WHEN** the extension lists the files to watch
- **THEN** it watches Chrome's `Local State`, each of the two profiles' five credential files, and the Firefox profile's three, and nothing in `System Profile` or `Crashpad`

#### Scenario: The browser's own reads are not reported

- **GIVEN** an open of Chrome's `Login Data` by a process signed by Chrome's team, and one by an unsigned process, an Apple tool, or another browser
- **WHEN** the extension judges each open
- **THEN** it drops the first and reports the others

#### Scenario: A directory past the bound is watched whole

- **GIVEN** a Chrome directory holding `Default` and 50 numbered profiles, and a Firefox directory holding 50 decoy profiles beside the real one
- **WHEN** the extension lists the files to watch
- **THEN** it watches both directories as prefixes and neither's files as literal paths, and of the opens a prefix delivers it reports only those of credential files, so an open of the real Firefox profile's `logins.json` is reported and one of its `places.sqlite` is not

### Requirement: An installer script names its package's signature

The agent SHALL attach to the exec of a package installer script the signature of the package the script belongs to: whether it is signed by a certificate macOS trusts, whether Apple's notary service accepted it, and the Developer ID team that signed it. The package is the one named by the script's first argument, which is where PackageKit passes it.

The agent SHALL do so only when the exec's parent is Apple's `package_script_service`, since the argument can be written by anyone and would otherwise let a script claim a signed package's identity. When the package cannot be read, the exec SHALL carry no package signature rather than one reporting it unsigned. The signature is read after the script has started, so a package changed after PackageKit prepared the install SHALL also leave the exec without one, since the file read may no longer be the package being installed. When the install has already ended by the time the package is read, so that nothing shows it unchanged, the agent SHALL attach the signature only if it is not one macOS trusts: that answer cannot lend trust, and omitting it would let a package with quick scripts escape being reported unsigned. Only a signature macOS accepts SHALL be reported as signed, and a team SHALL be reported only from a Developer ID Installer certificate.

#### Scenario: A script PackageKit ran carries its package's signature

- **GIVEN** an exec whose parent is `package_script_service` and whose script runs out of PackageKit's sandbox with a package path as its first argument
- **WHEN** the agent enriches the event
- **THEN** the exec carries that package's signature

#### Scenario: A script from another parent carries none

- **GIVEN** an exec whose arguments name a signed vendor package after a sandbox-shaped script path, but whose parent is a shell
- **WHEN** the agent enriches the event
- **THEN** the exec carries no package signature and the package is not read

#### Scenario: An unreadable package is not reported as unsigned

- **GIVEN** an installer script's exec whose package can no longer be read
- **WHEN** the agent enriches the event
- **THEN** the exec carries no package signature

#### Scenario: A late read reports only an untrusted package

- **GIVEN** an installer script's exec read after PackageKit removed the install sandbox
- **WHEN** the agent enriches the event
- **THEN** an unsigned or untrusted package's signature is attached, reported as not signed
- **AND** a package macOS trusts is not reported, since nothing shows it is the package that was installed

#### Scenario: A package changed during the install is not classified

- **GIVEN** an installer script's exec whose package file was replaced after PackageKit created the install sandbox
- **WHEN** the agent enriches the event
- **THEN** the exec carries no package signature

### Requirement: TCC permission changes are reported

The security extension SHALL emit a `tcc_modify` event when macOS reports that a TCC permission record was created, modified or deleted. The payload SHALL carry the service, the identity of the app the permission is about and its identity type, the update type, the resulting right, the reason, and the instigating process's pid. It SHALL carry the instigating process's code signing when macOS reports that process, the responsible process's pid when macOS reports a responsible audit token, and the responsible process's code signing when macOS reports that process; each is omitted, not sent as null, when absent. The agent SHALL add the app's path and its on-disk code signing, as `identity_path` and `identity_code_signing`, when the identity is an executable path, or a bundle identifier for which LaunchServices has an application, and the app can be read; otherwise it SHALL leave both out. When several installed copies share the bundle identifier, the agent SHALL report the least trusted readable one: the first whose signature is not an Apple platform binary's, or Apple's only when every readable copy is. The identity type, update type, right and reason SHALL be sent as names, and a value the extension does not know SHALL be sent as `unknown`.

#### Scenario: The change is named in words

- **GIVEN** a TCC modification whose SDK values are a bundle identifier, a deletion, an unknown right and no reason
- **WHEN** the extension serializes it
- **THEN** the payload says `bundle_id`, `delete`, `unknown` and `none`, and a reason value past the SDK's last known case is sent as `unknown`

#### Scenario: The app a permission is about carries its signature

- **GIVEN** a `tcc_modify` whose identity is a bundle identifier LaunchServices knows, one whose identity is an executable path, and one whose identity no app claims
- **WHEN** the agent enriches the events
- **THEN** the first two carry the app's path and its code signing, and the third carries neither
- **AND** when an Apple copy and a copy that is not Apple's share one identifier, the copy that is not Apple's is reported

### Requirement: A watched path can name every user's home

A watched path that starts with `~/` SHALL name the same path in every user's home folder. The extension SHALL accept such an entry under the rules it applies to an absolute path, judged as the path it would be in a home at the root, so that a prefix names something at least two components below the home. It SHALL expand the entry into one path in each home it reads from the directory service: root's, and each account with a user ID of 500 or above whose home is an absolute path other than a placeholder, once each. An expansion that the extension would not watch as an absolute path SHALL be left out. The extension SHALL re-read the accounts at least every five minutes and re-apply the set when they have changed, so an account added after the set was pushed is watched without another push.

#### Scenario: A home entry is judged below the home

- **GIVEN** pushed entries `~/.ssh/authorized_keys` (literal), `~/Library/LaunchAgents/` (prefix) and `~/Library/` (prefix)
- **WHEN** the extension decodes the set
- **THEN** it accepts the first two and refuses `~/Library/`, as it refuses `/Library/`

#### Scenario: A home entry covers root and every person's home

- **GIVEN** accounts for root, two people, a system account, and an account whose home is the `/var/empty` placeholder
- **WHEN** the extension expands `~/.ssh/authorized_keys`
- **THEN** it watches the file in root's home and in each person's home, once each, and in no other

### Requirement: A snapshot exec carries the running process's signature

A synthetic `exec` event emitted at startup for a process that already existed SHALL carry the code signature of that process's running code, in the same `code_signing` shape a live exec carries: its signing identifier, its team, the kernel's code-signing flags, and whether it is a platform binary. The signature SHALL be read from the running code rather than the file on disk, which may have been replaced since the process started, and reading it SHALL NOT contact the network.

A process can exit and its pid be reused between listing the process table and reading the signature, so the system SHALL attribute a signature only when the process holding the pid started when the listed one did, and SHALL otherwise emit the event without a signature. An unsigned process SHALL be emitted without a signature, as a live exec of one is. A process whose running code the kernel no longer holds validly signed SHALL also be emitted without one: exclusions trust a team and signing identifier, so an identity SHALL be attributed only to code that is still valid, and anything else is judged as unsigned, which reports it rather than excludes it.

Without the signature, every judgement made on it fails for any process that predates the extension, which is every process started at boot and every one alive across an agent upgrade: a rule's built-in skip of Apple's indexing services, and an operator's `team_id` or `signing_id` exclusion, so the process is reported instead.

#### Scenario: An Apple daemon carries its signature

- **GIVEN** an Apple platform daemon, such as Spotlight's `mds`, that was running when the extension started
- **WHEN** the extension emits its startup snapshot
- **THEN** the daemon's `exec` event carries its signing identifier, no team, its code-signing flags, and `is_platform_binary = true`

#### Scenario: An unsigned process carries no signature

- **GIVEN** a running process whose code carries no signing identifier
- **WHEN** the extension emits its startup snapshot
- **THEN** that process's `exec` event carries no `code_signing`

#### Scenario: Invalid code carries no identity

- **GIVEN** a running process whose code claims a team and signing identifier but which the kernel no longer holds validly signed
- **WHEN** the extension emits its startup snapshot
- **THEN** that process's `exec` event carries no `code_signing`

#### Scenario: A reused pid gets no signature

- **GIVEN** a process listed in the startup snapshot whose pid is taken by a different process before its signature is read
- **WHEN** the extension reads the signature
- **THEN** the `exec` event carries no `code_signing` rather than the new process's

### Requirement: Agent-scoped file activity

The extension SHALL report file creates, modified closes, renames and deletes by an AI agent's processes as `file_activity` events, and SHALL NOT report them for any other process. An agent subtree SHALL start at an exec of the Claude Code binary, matched by signing identifier, or of a process carrying `CLAUDE_CODE_SESSION_ID`, and SHALL extend to every process it forks, from the fork. Each flush SHALL send at most 64 events.

#### Scenario: An agent process is observed from its exec

- **GIVEN** the extension is running
- **WHEN** the Claude Code binary execs, or a process carrying `CLAUDE_CODE_SESSION_ID` does
- **THEN** its later file operations arrive as `file_activity` events naming its `pid` and `pidversion`

#### Scenario: The observed set follows the subtree

- **GIVEN** an observed process
- **WHEN** it forks, and the child execs at once and writes a file
- **THEN** the child's write is reported, under the generation its exec created
- **AND** the child is released when it exits

#### Scenario: A process outside an agent subtree is not observed

- **GIVEN** a process that is neither an agent root nor forked from an observed process
- **WHEN** it creates, changes, renames or deletes files
- **THEN** no `file_activity` event names it

#### Scenario: Repeats of one file fold into one row

- **GIVEN** an observed process changes one file several times within a second
- **WHEN** the window is flushed
- **THEN** one event carries the file with `coalesced_count` and `last_timestamp_ns`, timestamped at the first change

#### Scenario: A busy window rolls up into directories

- **GIVEN** an observed build creates thousands of files within a second
- **WHEN** the window is flushed
- **THEN** at most 64 events are sent, the deepest paths folded into their directories with `rolled_up` set
- **AND** every operation is counted in an event or, past the top level, logged as dropped

#### Scenario: A terminal is not file activity

- **GIVEN** an observed shell command writes to its terminal
- **WHEN** it exits and closes the terminal device
- **THEN** no `file_activity` event names a path under `/dev/`

### Requirement: Agent tool call origin

When a transcript line records a tool call under a `session_id` other than its session's, the agent SHALL send that id as the call's `origin_session_id`. A line whose `session_id` is its session's, or absent, SHALL carry none.

#### Scenario: A copied call names the session it was written under

- **GIVEN** a forked session's transcript, whose copy of its parent's tool call keeps the parent's `session_id`
- **WHEN** the agent reads the line
- **THEN** it reports the call under the fork's session with `origin_session_id` naming the parent, and reports the fork's own calls with none

### Requirement: Fork parent from the job

When a session's Claude Code job (`~/.claude/jobs/<first eight characters of the session id>/parent-transcript.jsonl`, written by `/fork` in 2.1.301 and later) names another session, the agent SHALL send it as `forked_from_session_id` on that session's tool calls, read within the user's home as a regular file, bounded, and never for a subagent's transcript.

#### Scenario: A fork's calls name the session its job was forked from

- **GIVEN** a session whose job's parent transcript carries another session's id, and sessions with no job, a job naming the session itself, or a FIFO or a link out of the home in its place
- **WHEN** the agent reads their transcripts
- **THEN** only the first session's calls carry `forked_from_session_id`, naming that other session

### Requirement: Agent session markers capture

The `exec` event SHALL carry, in an `env` object, the exec'd process's environment variables whose full name is on the agent session marker allowlist. Names SHALL be matched in full, never by prefix, and no other variable SHALL be sent. A value longer than 256 UTF-8 bytes SHALL be dropped. The `env` key SHALL be omitted when no marker is present.

#### Scenario: An agent-spawned process carries its session

- **GIVEN** the endpoint event capture is running
- **WHEN** a Claude Code session runs a command
- **THEN** the command's `exec` events carry `env.CLAUDE_CODE_SESSION_ID`
- **AND** its value equals the name of the session's transcript file

#### Scenario: A credential never leaves the host

- **GIVEN** a process whose environment holds `CLAUDE_CODE_MESSAGING_TOKEN` beside the session markers
- **WHEN** it execs
- **THEN** the `exec` event carries the markers
- **AND** neither the token's name nor its value appears in the event

#### Scenario: An oversized marker is dropped

- **GIVEN** a process whose environment holds an allowlisted marker longer than 256 UTF-8 bytes
- **WHEN** it execs
- **THEN** the `exec` event omits that marker rather than truncating it

#### Scenario: A process without markers keeps its shape

- **GIVEN** a process whose environment holds no allowlisted marker
- **WHEN** it execs
- **THEN** the `exec` event has no `env` key
- **AND** a payload that carries `env` still decodes on a server that does not read it

### Requirement: Agent socket connects

The extension SHALL report each connect by a process in an AI agent's subtree to a Unix-domain socket as a `file_activity` row with operation `connect` and the socket file's path, folded and bounded as the subtree's file operations are. It SHALL NOT report connects by processes outside agent subtrees, and SHALL NOT attribute the daemon's own activity to the session.

#### Scenario: A session's connect to a daemon's socket is reported

- **GIVEN** a process an AI agent started connects to `/var/run/docker.sock` three times within one second
- **WHEN** the extension folds the window
- **THEN** it sends one `file_activity` row with operation `connect`, path `/var/run/docker.sock` and `coalesced_count` 3, apart from any other operation on that path

### Requirement: Agent tool call capture

The agent SHALL report each tool call an AI agent records in its session transcript as an `agent_tool_call` event, stamped at the transcript's time, carrying the session id, `tool_use_id`, tool name, a Bash call's command and description, and a file tool's path. It SHALL NOT send prompt text, model output or tool results. A line it cannot read, or naming a session not its transcript's, SHALL be skipped; each call SHALL carry its transcript owner's uid.

#### Scenario: A Bash tool call is reported with its command

- **GIVEN** a Claude Code session on the host
- **WHEN** the agent runs a Bash command
- **THEN** an `agent_tool_call` event carries the session id, the call's `tool_use_id`, `Bash`, and the command
- **AND** a command longer than 8 KiB is cut on a character boundary and marked `truncated`

#### Scenario: Prompts and results never leave the host

- **GIVEN** a transcript holding the user's prompts, the model's text and the tools' results beside its tool calls
- **WHEN** the agent reads it
- **THEN** only the tool calls produce events, and none carries a prompt, model text or a tool result

#### Scenario: An unreadable line is skipped, not guessed

- **GIVEN** a transcript line that is not JSON, or a tool call missing its session, id, name or time
- **WHEN** the agent reads it
- **THEN** no event is sent for that line and the reading continues with the next

#### Scenario: A forged line cannot stand in for another session's call

- **GIVEN** a user writes into their own transcript a line carrying another session's id and one of its call ids
- **WHEN** the agent reads it
- **THEN** no event is sent for that line
- **AND** a call is stamped with its transcript owner's uid, which its event id is derived from, so another user's line can never take its event id

### Requirement: Exec strings read by token length

The extension SHALL read an exec event's path and each argument by the length of its Endpoint Security string token and never past it. A token holding no bytes, or bytes that are not valid UTF-8, SHALL be reported as the empty string.

#### Scenario: A token is read to its length and no further

- **GIVEN** a string token whose bytes are not followed by a NUL
- **WHEN** the extension reads it
- **THEN** it reads exactly the token's length, keeping a NUL inside the token as part of it

#### Scenario: A token that is not UTF-8 reads empty

- **GIVEN** a string token whose bytes are not valid UTF-8
- **WHEN** the extension reads it
- **THEN** it reads as the empty string, not as text with replacement characters

### Requirement: Exit and file event process generation

The `exit` event SHALL carry the exiting process's `pidversion`. The `open`, `file_rename`, `file_truncate` and `file_delete` events SHALL carry the acting process's `pidversion`. Each value SHALL equal the `pidversion` of the exec event that started the running image, or of the fork event when the process never exec'd. The field is optional on the wire.

#### Scenario: An exit names the generation that exited

- **GIVEN** the endpoint event capture is running
- **WHEN** a process execs and later exits
- **THEN** the `exit` event's `pidversion` equals the `pidversion` on that process's exec event

#### Scenario: A file event names the generation that acted

- **GIVEN** the endpoint event capture is running
- **WHEN** a process execs and then truncates, deletes, renames or writes a watched file
- **THEN** the file event's `pidversion` equals the `pidversion` on that process's exec event

#### Scenario: A payload without a generation keeps its shape

- **GIVEN** an exit or file payload built without a `pidversion`
- **WHEN** it is serialized
- **THEN** it has no `pidversion` key

### Requirement: Launchd job definitions

On a `launchctl load` or `bootstrap` exec, the agent SHALL send the capped `Label`, `Program` and `ProgramArguments` of each plist it names, or of the `.plist` files directly in a directory it names, at most eight, as `launchd_jobs`. It SHALL read a plist as the user who ran launchctl, without supplementary groups, at most 64 KiB of it, and SHALL hold its event loop at most a bounded time for the read, sending the exec without what it could not read in time.

#### Scenario: A launchctl load carries the jobs it names

- **GIVEN** `launchctl load -w -S Aqua /tmp/a.plist b.plist unreadable.plist` run in `/Users/v/proj`
- **WHEN** the agent handles the exec
- **THEN** it carries the definitions of `/tmp/a.plist` and `/Users/v/proj/b.plist`, and none for the plist it could not read

#### Scenario: A plist the user could not read is not read for them

- **GIVEN** a plist mode 0600 owned by one user
- **WHEN** another user runs `launchctl load` on it, directly or through a symlink
- **THEN** the agent does not read it, though it reads a world-readable plist for anyone and a user's own plist for its owner

#### Scenario: A blocked plist read does not hold back telemetry

- **GIVEN** a plist read that does not finish, as on a stalled mount or a named pipe held open
- **WHEN** the agent handles the exec, and another launchctl exec while that read still runs
- **THEN** the first waits no longer than the bound and the second not at all, both go out without the definitions, and reads run again once the stuck one ends

#### Scenario: What is no plist yields no job

- **GIVEN** a `launchctl load` naming a file over 64 KiB, a missing path, a directory the user cannot list, or an exec whose uid or gid is outside a uid's 32-bit range
- **WHEN** the agent handles the exec
- **THEN** it reads no job for that operand, and the exec goes out without one for it

#### Scenario: A directory operand carries the jobs of the plists in it

- **GIVEN** `launchctl load agents` run in a directory whose `agents` holds `a.plist`, `b.plist`, `notes.txt` and a `nested/` subdirectory with a plist
- **WHEN** the agent handles the exec
- **THEN** it carries the jobs of `a.plist` and `b.plist`, listed and read as the user, as launchctl itself loads, and none from `notes.txt` or `nested/`

### Requirement: Process lineage identity capture

The `fork` event SHALL carry the forking process's `parent_pidversion`. The `exec` event SHALL carry `original_ppid`, the `parent_pidversion` of the parent its `ppid` names, `responsible_pid`, `responsible_pidversion`, `session_id`, `group_id`, `tty` when the process has a controlling terminal, `start_time_ns`, and the working directory in `cwd`. Each lineage field is optional on the wire and SHALL be omitted when unknown.

#### Scenario: A fork names the generation of its creator

- **GIVEN** the endpoint event capture is running
- **WHEN** a process forks a child
- **THEN** the `fork` event carries `parent_pid` and `parent_pidversion` equal to the forking process's own pid and generation
- **AND** they identify the creator even after the child is reparented to launchd

#### Scenario: A daemonized process still names its creator

- **GIVEN** the endpoint event capture is running
- **WHEN** a shell starts a command with a double fork and the command execs after its intermediate parent has exited
- **THEN** the command's `exec` event carries `ppid` 1, because launchd adopted it
- **AND** it carries the intermediate parent's pid in `original_ppid`

#### Scenario: An exec carries its grouping identity

- **GIVEN** the endpoint event capture is running
- **WHEN** a process execs
- **THEN** the `exec` event carries `session_id`, `group_id`, `start_time_ns`, `responsible_pid`, and `responsible_pidversion`
- **AND** it carries `tty` only when the process has a controlling terminal
- **AND** `cwd` holds the process's working directory at exec

#### Scenario: A snapshot row carries what the process table knows

- **GIVEN** the extension has started and enumerates the processes already running
- **WHEN** it emits a synthetic `exec` event for one of them
- **THEN** the event carries the process's `group_id` and `start_time_ns`
- **AND** it omits the lineage fields the process table cannot supply

#### Scenario: An event without lineage keeps its shape

- **GIVEN** a payload built without any lineage field
- **WHEN** it is serialized
- **THEN** none of the lineage keys appear in it
- **AND** a payload that carries them still decodes on a server that does not read them
