package api

import (
	"context"
	"errors"

	"github.com/rawtrail/rawtrail/server/httpserver"
)

// EventArchive is the durable, append-mostly event lake: the source of truth for per-process correlation and, in v0.5.0, hunting and
// investigation queries. Ingestion writes every accepted event here; the archive ages events out by a time-based retention window.
//
// Invariants every implementation MUST uphold:
//   - Durable before ack: an event is considered persisted only once Insert returns nil.
//   - Idempotent by EventID: re-inserting an event with a known EventID never surfaces a duplicate in query results and never alters
//     the stored content, so at-least-once delivery is safe.
//
// The v0.4.0 implementation is ClickHouse; reads serve correlation today and hunting later. Writes are batched, never synchronous
// per request, so the interface says nothing about per-call batching.
type EventArchive interface {
	// Insert durably stores events in the archive. Idempotent by EventID.
	Insert(ctx context.Context, events []Event) error

	// NetworkEventsForProcess returns the network_connect and dns_query events attributed to (hostID, pid) within tr, ordered by
	// timestamp. Cross-stream correlation rules consume it to join a process's DNS resolutions with its outbound connections, keyed on
	// pid alone because they re-correlate the results in memory.
	//
	// The process-detail view uses NetworkEventsForGeneration instead: a view that names ONE generation must not answer with a
	// sibling generation's flows.
	NetworkEventsForProcess(ctx context.Context, hostID string, pid int, tr httpserver.TimeRange) ([]Event, error)

	// NetworkEventsForGeneration returns the network_connect and dns_query events belonging to ONE process generation, ordered by
	// timestamp. See ProcessFlowFilter for how a flow is attributed to a generation.
	//
	// The bool reports that the row cap dropped rows. Unlike EventsByTypeForHost this truncates rather than erroring: the caller is an
	// operator panel reading what a process DID, so a partial list plus an honest "there are more" beats blanking the view, whereas a
	// rule reasoning about what is ABSENT cannot tell a missing event from a dropped one and must get the error instead.
	NetworkEventsForGeneration(ctx context.Context, filter ProcessFlowFilter) ([]Event, bool, error)

	// EventsByTypeForHost returns one host's events of a single type whose EVENT time falls inside tr, oldest first. Narrow by
	// construction: (host_id, event_type, timestamp_ns) is the archive's sorting-key prefix, so the read is a primary-key range
	// scan rather than a filter over the host's whole stream.
	//
	// Event time, not ingest time, because the caller (the sensor-tamper rule) measures the gap BETWEEN two events from the same
	// producer on one host. Those share a clock, so event time is the honest measure of that gap, and it is also the indexed one.
	// The cross-stream reads above bound on ingest time instead because they join two different producers whose clocks drift.
	//
	// Bounded, and the bound is REPORTED: a result larger than the implementation's cap returns ErrEventsTruncated rather than a
	// truncated page, because a caller reasoning about what is ABSENT cannot tell a missing event from a dropped one.
	EventsByTypeForHost(ctx context.Context, hostID, eventType string, tr httpserver.TimeRange) ([]Event, error)

	// EventsByIDs returns the full envelopes for the given event_ids, ordered by (timestamp_ns, event_id). Alert evidence capture uses
	// it to snapshot a finding's triggering events into durable per-alert storage (alert_event_payloads) that outlives the archive's
	// retention window. IDs with no surviving event (already aged out) are omitted rather than erroring, so capture stays best-effort.
	EventsByIDs(ctx context.Context, eventIDs []string) ([]Event, error)

	// SearchEvents runs the fleet-wide hunting search over the archive (issue #582): events of filter.EventType whose artifact value
	// matches (remote address for network_connect, query name for dns_query), newest-first, keyset-paged from cursor. Returns up to
	// limit events, a next cursor when more remain, and total_matched (the full match count independent of the page). A malformed
	// cursor is a caller error.
	SearchEvents(ctx context.Context, filter EventSearchFilter, cursor string, limit int) (EventSearchResult, error)

	// TelemetryActivityForHosts reports, per host, whether each telemetry stream produced anything inside tr. It answers a
	// question about ABSENCE (issue #677: a network-extension provider that wedges keeps reporting itself healthy, and the only
	// evidence is that its events stopped arriving while process telemetry kept flowing), so it counts rather than returning
	// events: the caller needs "did this stream produce anything", not the events themselves.
	//
	// Hosts with no events in the range are ABSENT from the result rather than present with zeroes. A caller must not read that
	// absence as silence: an unknown host and a host that produced nothing are indistinguishable here, and neither is evidence of
	// a wedge (the wedge claim requires positive process activity, which an absent host by definition lacks).
	TelemetryActivityForHosts(ctx context.Context, hostIDs []string, tr httpserver.TimeRange) (map[string]TelemetryActivity, error)

	// HostTimeline returns one host's investigation events (TimelineEventTypes) interleaved in event-time order,
	// newest-first, over the filter's event-time window, optionally narrowed to a subset of those types and to a case-insensitive
	// payload substring (issue #583). Keyset-paged over (timestamp_ns, event_id) from cursor, up to limit events, with a next cursor
	// when more remain and total_matched independent of the page. Shares the EventSearchResult page shape and EventCursor codec with
	// SearchEvents. A malformed cursor is a caller error.
	HostTimeline(ctx context.Context, filter HostTimelineFilter, cursor string, limit int) (EventSearchResult, error)

	// ActivityForGenerations rolls up what a set of process generations touched on one host (issue #1285): the files they opened,
	// renamed, truncated or deleted, the destinations they connected to, and the names they resolved. Events are matched to a
	// generation by exact (pid, pidversion), the rule the alert-chain timeline uses, so an event without a pidversion, or one of a
	// later process that reused a pid, is never counted. See ActivityRollup for what each class reports.
	ActivityForGenerations(ctx context.Context, filter ActivityFilter) (ActivityRollup, error)

	// ActivityTotalsForGenerations is ActivityForGenerations' totals without its lists (issue #1422), with the file events counted by
	// operation, for a summary that names how much a scope touched but none of what.
	ActivityTotalsForGenerations(ctx context.Context, filter ActivityFilter) (ActivitySummary, error)

	// ActivityPage lists one class of the rollup's rows (issue #1424), filter.Limit of them, from after the row a cursor names, or
	// from the first when the cursor is empty: the page that follows the rollup's own list, or one after it. The rows are in the
	// rollup's order. A cursor of another class, or one that does not decode, is ErrInvalidActivityCursor.
	ActivityPage(ctx context.Context, filter ActivityFilter, class, cursor string) (ActivityPage, error)

	// SessionTouches lists, for one process generation, the first time it touched a path of each AI agent session (issue #1319),
	// from its agent file activity: Claude Code's transcript ~/.claude/projects/<project>/<session id>.jsonl, and the session's
	// directories.
	SessionTouches(ctx context.Context, hostID string, generation ProcessGeneration) ([]SessionTouch, error)

	// AgentToolCalls lists the tool calls an AI agent session recorded on one host (issue #1288), oldest first, one per tool_use_id and
	// transcript owner however many times the agent reported it, and reports whether more than the limit were recorded. A call that
	// is a copy of another session's (see AgentSessionLineage) is not listed: it is that session's.
	AgentToolCalls(ctx context.Context, filter AgentToolCallFilter) ([]AgentToolCall, bool, error)

	// AgentPrompts lists the prompts a person typed in one AI agent session on one host (issue #1426), oldest first, a page at a time
	// after the filter's cursor, keeping those whose text holds the filter's search when it has one. A cursor that does not decode is
	// an ErrInvalidPromptCursor.
	AgentPrompts(ctx context.Context, filter AgentPromptFilter) (AgentPromptPage, error)

	// AgentUsageTotals totals the tokens one AI agent session's messages used on one host (issue #1427), by model, and counts its
	// messages by the permission mode in force, each message once by its id and owner. A message that is a copy of another session's
	// (see AgentToolCalls) is that session's and is not counted. The filter's window and Limit are ignored.
	AgentUsageTotals(ctx context.Context, filter AgentToolCallFilter) (AgentUsageTotals, error)

	// AgentToolCallCounts counts the calls AgentToolCalls would list for the filter, uncapped, by tool name (issue #1422), most used
	// first and then by name. The filter's Limit is ignored.
	AgentToolCallCounts(ctx context.Context, filter AgentToolCallFilter) ([]ToolCallCount, error)

	// AgentSessionLineage reads which session a session was forked from and which were forked from it (issue #1347), from the copies
	// of each other's tool calls their transcripts hold. Only calls read from transcripts ownerUID owns count: the session's owner,
	// as its processes show.
	AgentSessionLineage(ctx context.Context, hostID, sessionID string, ownerUID int64) (AgentSessionLineage, error)
}

// TelemetryActivity is one host's per-stream event presence over a telemetry-freshness window.
//
// The counts are NOT deduplicated against the archive's ReplacingMergeTree duplicates, and deliberately so: every decision made from
// this type is "did this stream produce anything at all", which a duplicate cannot flip in either direction. Only ProcessInWindow is
// read as a magnitude, and only to describe the contradiction to a human, where an occasional re-inserted event is immaterial.
type TelemetryActivity struct {
	// ProcessInWindow counts exec and fork events: the evidence the host is awake and doing work, which is what makes an empty
	// flow stream a contradiction rather than an idle machine.
	ProcessInWindow int64
	// ConnectInWindow and DNSInWindow count each flow stream. Zero is the signal.
	ConnectInWindow int64
	DNSInWindow     int64
}

// HostTimelineFilter selects events for one host's merged event timeline (issue #583). HostID is required. FromNs/ToNs bound event
// time (timestamp_ns, what the host page's time window means), zero meaning unbounded on that side. EventTypes narrows to a subset of
// the timeline event classes (empty means all of them). Text, when non-empty, keeps only events whose raw payload contains it
// case-insensitively. Chain, when non-empty, scopes the timeline to a set of process generations (the alert chain), so it mirrors the
// graph's focus. Empty means no chain scope (the full host stream).
type HostTimelineFilter struct {
	HostID     string
	FromNs     int64
	ToNs       int64
	EventTypes []string
	Text       string
	Chain      []ProcessGeneration
}

// ProcessGeneration identifies one process generation by its pid AND kernel pid-generation (pidversion, audit_token_to_pidversion). The
// pair is unique across PID reuse: pid 450 may be bash then curl then orbit within one window, each a distinct pidversion, so matching
// (pid, pidversion) scopes to the exact generation the graph shows. This is robust to ingest timing (a short-lived process whose fork
// and exit land in one batch still matches all its events) where an ingest-time window would collapse to near-zero and drop them.
type ProcessGeneration struct {
	PID        int64
	PIDVersion int64
}

// ProcessFlowFilter selects the network_connect and dns_query events belonging to ONE process generation, for the process-detail view
// (issue #716). A flow is attributed to the generation when EITHER of two arms matches:
//
//   - Identity: the flow's payload carries a pidversion and (pid, pidversion) equals this generation's. This arm is deliberately NOT
//     bounded by IngestWindow. The pair is unique across PID reuse, so no window is needed to disambiguate, and requiring one is the
//     #716 defect: a flow and its process's exit travel up the agent uploader in separate batches, so a short-lived process's flow
//     routinely ingests seconds AFTER the exit and falls outside any tight window.
//   - Legacy: the flow's payload carries NO pidversion (a pre-#403 agent, or a flow whose audit token was unavailable), and its
//     ingest time falls inside IngestWindow. Identity cannot speak for these, so the window remains the only available evidence.
//
// PIDVersion nil means the PROCESS row predates pidversion capture. Identity is unavailable on this side too, so ONLY the legacy arm
// applies: a candidate must itself carry no pidversion. A flow that carries one belongs to some generation of this pid, and a row that
// cannot name a generation must not claim it on timing alone. Doing so is the #716 mis-attribution class surviving on the legacy side.
//
// Life bounds the identity arm on EVENT time. Identity is not guaranteed unique: rows written before #715 repeated one pidversion
// across the generations of a re-exec chain, so (pid, pidversion) can match two generations and each would otherwise show the other's
// flows. Event time is the right discriminator and ingest time is not: a flow's event time always falls inside the life of the process
// that made it, whereas its ingest time routinely lands after that process exited, which is the whole of #716. Callers pad Life for
// the network-extension versus Endpoint Security clock difference (issue #7).
//
// Bound applies to every candidate row in both arms. It exists to keep the scan pruned rather than to decide attribution, so it is
// wide where IngestWindow is tight; an implementation MUST NOT narrow attribution with it. In particular it MUST NOT be derived from
// the process's own age: a daemon older than the retention window would then be unable to show any flow at all.
//
// Limit caps the returned rows. Flows are agent-supplied and a long-lived process on a noisy host can hold far more than a panel can
// render, so the read is bounded and reports whether the cap dropped rows rather than silently shortening.
type ProcessFlowFilter struct {
	HostID string
	PID    int
	// PIDVersion is the generation's kernel pid generation, nil when the process row carries none.
	PIDVersion *int64
	// Bound is the wide ingest bound applied to every candidate row, for scan pruning only.
	Bound httpserver.TimeRange
	// IngestWindow is the tight ingest window that attributes flows the identity arm cannot speak for.
	IngestWindow httpserver.TimeRange
	// Life is the generation's event-time window, padded for clock skew, used to disambiguate a repeated identity.
	Life httpserver.TimeRange
	// Limit caps the rows returned. Non-positive means the implementation's own default cap.
	Limit int
}

// EventSearchFilter selects events for the fleet-wide connection/DNS search (issue #582). EventType picks the artifact class
// (network_connect matches on remote address, dns_query on query name); Value is the exact artifact to match. HostID empty means every
// host. FromNs/ToNs bound ingest time; zero means unbounded on that side.
type EventSearchFilter struct {
	EventType string
	Value     string
	HostID    string
	FromNs    int64
	ToNs      int64
}

// EventSearchResult is one page of a fleet-wide event search plus pagination and total metadata. NextCursor is empty on the last
// page; TotalMatched counts every matching event independent of the page, or is TotalNotCounted when the count was deliberately
// skipped (see below).
type EventSearchResult struct {
	Events       []Event `json:"events"`
	NextCursor   string  `json:"next_cursor,omitempty"`
	TotalMatched int64   `json:"total_matched"`
}

// TotalNotCounted marks TotalMatched as "not computed". The recent-events listing (an event search with no artifact value) skips the
// COUNT query, which on a large archive is the more expensive half of the page and yields a grand total of only marginal value for a
// browse view; pagination is driven by NextCursor, not the total, so nothing depends on it. A filtered search still reports the exact
// count. The UI shows "Showing N" instead of "Showing N of M" when it sees this sentinel.
const TotalNotCounted int64 = -1

// ErrEventsTruncated is returned by a bounded read that matched more rows than its cap. It exists so a caller cannot mistake a
// truncated result for a complete one. The sensor-tamper rule is why: it decides "capture never came back" from the ABSENCE of a
// recovery in its window, so a silently truncated page would let a host that recovered be reported as tampered with, and the more
// events a host emits the likelier that becomes. Failing the read loudly turns a hostile or noisy agent into an error an operator
// sees rather than a false accusation against that host.
var ErrEventsTruncated = errors.New("visibility: event read exceeded its row cap")

// ErrInvalidActivityCursor is returned by ActivityPage when its cursor does not decode or names another class. The operator handler
// maps it to 400.
var ErrInvalidActivityCursor = errors.New("visibility: invalid activity cursor")

// ErrInvalidPromptCursor is AgentPrompts' answer to a cursor that does not decode.
var ErrInvalidPromptCursor = errors.New("visibility: invalid prompt cursor")

// Activity classes, as ActivityPage names them.
const (
	ActivityFiles   = "files"
	ActivityNetwork = "network"
	ActivityDNS     = "dns"
)

// ActivityPage is one page of one activity class's rows: the class's rows, the others nil, and the cursor of the next page, empty on
// the last.
type ActivityPage struct {
	Files      []FileActivityRow
	Network    []NetworkActivityRow
	DNS        []DNSActivityRow
	NextCursor string
}

// ErrInvalidEventCursor is returned by SearchEvents when the pagination cursor does not decode. The operator handler maps it to 400.
var ErrInvalidEventCursor = errors.New("visibility: invalid event search cursor")

// ActivityFilter selects one host's events of a set of process generations for an activity rollup (issue #1285). Limit caps the
// rows each class lists; non-positive means the implementation's default.
type ActivityFilter struct {
	HostID      string
	Generations []ProcessGeneration
	// Spans are generations whose events count only inside a span of time (issue #1319): an AI agent process that served several
	// sessions belongs to each only while it served it. A generation is listed in Generations or in Spans, not both.
	Spans []GenerationSpan
	// FromNs, when positive, keeps only events at or after it: the earliest start of the scope's processes. No process of the scope
	// can have an event before it, so the read skips the host's older history, and an older process that shared a generation, as one
	// before a reboot can, is not counted.
	FromNs int64
	Limit  int
	// Search, when set, keeps only the rows of an ActivityPage whose artifact holds it, case-insensitively: a file's path or a rename's
	// origin, a destination's address or name, a lookup's name (issue #1424). The rollup's totals and lists do not read it.
	Search string
}

// GenerationSpan is a process generation and the span its events are counted in: from FromNs, up to but excluding ToNs, zero ToNs
// meaning open-ended.
type GenerationSpan struct {
	ProcessGeneration
	FromNs int64
	ToNs   int64
}

// SessionTouch is the first time a process generation created or changed a path an AI agent names after a session (issue #1319):
// its transcript, or one of its directories.
type SessionTouch struct {
	SessionID string
	FirstNs   int64
}

// ActivityRollup is what a set of process generations touched, by class.
type ActivityRollup struct {
	Files   FileActivity    `json:"files"`
	Network NetworkActivity `json:"network"`
	DNS     DNSActivity     `json:"dns"`
}

// ActivityTotals is a class's size: Events counts its events, a coalesced flow counting as the connections it stands for, and
// Distinct counts its distinct artifacts (paths, destinations, names) across every process. Truncated reports that the class
// listed fewer rows than it has.
type ActivityTotals struct {
	Events    int64 `json:"events"`
	Distinct  int64 `json:"distinct"`
	Truncated bool  `json:"truncated"`
}

// ClassTotals is a class's size without the report of a cut list: Events counts its events, a coalesced one counting as the
// occurrences it stands for, and Distinct its distinct artifacts.
type ClassTotals struct {
	Events   int64 `json:"events"`
	Distinct int64 `json:"distinct"`
}

// ActivitySummary is how much a set of process generations touched, by class, with no list of what (issue #1422).
type ActivitySummary struct {
	Files   FileTotals  `json:"files"`
	Network ClassTotals `json:"network"`
	DNS     ClassTotals `json:"dns"`
}

// FileTotals is the file class's totals and its events by operation, keyed as FileActivityRow.Operation is. A coalesced event counts
// as the operations it stands for, as in Events, so the operations sum to Events.
type FileTotals struct {
	ClassTotals
	ByOperation map[string]int64 `json:"by_operation"`
}

// ToolCallCount is how many calls of one tool an AI agent session recorded.
type ToolCallCount struct {
	ToolName string `json:"tool"`
	Count    int64  `json:"count"`
}

// ActivityRow is the part of a listed artifact every class shares: the process generation that touched it, how many times, and when.
// LastNs is the latest event time, a coalesced flow's last occurrence included.
type ActivityRow struct {
	PID        int64 `json:"pid"`
	PIDVersion int64 `json:"pidversion"`
	Count      int64 `json:"count"`
	FirstNs    int64 `json:"first_ns"`
	LastNs     int64 `json:"last_ns"`
}

// FileActivity lists, per process, the paths touched and how. Operation is the event type of a watched-path event (open, file_rename,
// file_truncate, file_delete), or the operation of an agent's file activity (create, modify, rename, delete); SourcePath is a single
// rename's origin, empty otherwise.
type FileActivity struct {
	ActivityTotals
	Rows []FileActivityRow `json:"rows"`
	// NextCursor reads the rows past the list, by ActivityPage, when the list was cut.
	NextCursor string `json:"next_cursor,omitempty"`
}

// FileActivityRow is one path one process touched in one way.
type FileActivityRow struct {
	ActivityRow
	Operation  string `json:"operation"`
	Path       string `json:"path"`
	SourcePath string `json:"source_path,omitempty"`
	// RolledUp marks Path as a directory standing for the agent file activity under it, which the extension folded to stay within its
	// per-second budget.
	RolledUp bool `json:"rolled_up,omitempty"`
}

// NetworkActivity lists, per process, the destinations it connected to.
type NetworkActivity struct {
	ActivityTotals
	Rows       []NetworkActivityRow `json:"rows"`
	NextCursor string               `json:"next_cursor,omitempty"`
}

// NetworkActivityRow is one destination one process connected to. RemoteHostname is the name the network extension reported for
// it, when it reported one.
type NetworkActivityRow struct {
	ActivityRow
	RemoteAddress  string `json:"remote_address"`
	RemotePort     int64  `json:"remote_port"`
	Protocol       string `json:"protocol"`
	RemoteHostname string `json:"remote_hostname,omitempty"`
}

// DNSActivity lists, per process, the names it resolved.
type DNSActivity struct {
	ActivityTotals
	Rows       []DNSActivityRow `json:"rows"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

// DNSActivityRow is one name one process resolved, with the addresses its answers carried.
type DNSActivityRow struct {
	ActivityRow
	QueryName string   `json:"query_name"`
	Addresses []string `json:"addresses"`
}

// AgentUsageTotals is what one AI agent session's messages used (issue #1427): its tokens by model, heaviest output first, and its
// messages by the permission mode in force when each was written, with the span each mode was seen over.
type AgentUsageTotals struct {
	Models []ModelUsage `json:"models"`
	Modes  []ModeUsage  `json:"modes"`
}

// ModelUsage is the tokens a session's messages written by one model used. Input excludes the tokens written to and read from the
// prompt cache, which CacheCreation and CacheRead count; Thinking is the part of Output spent thinking.
type ModelUsage struct {
	Model         string `json:"model"`
	Messages      int64  `json:"messages"`
	Input         int64  `json:"input_tokens"`
	Output        int64  `json:"output_tokens"`
	CacheCreation int64  `json:"cache_creation_input_tokens"`
	CacheRead     int64  `json:"cache_read_input_tokens"`
	Thinking      int64  `json:"thinking_tokens"`
}

// ModeUsage is how many of a session's messages were written in one permission mode, as the agent recorded it, and the first and
// last of them. An empty Mode is messages written before the transcript named any.
type ModeUsage struct {
	Mode     string `json:"mode"`
	Messages int64  `json:"messages"`
	FirstNs  int64  `json:"first_ns"`
	LastNs   int64  `json:"last_ns"`
}

// AgentPromptFilter selects a page of one session's prompts: the session on its host, the previous page's cursor (empty for the
// first), a search the text must hold (empty for none), and the page size.
type AgentPromptFilter struct {
	HostID    string
	SessionID string
	Cursor    string
	Search    string
	Limit     int
}

// AgentPrompt is one prompt a person typed, as the agent sent it (issue #1426): redacted and cut at 16 KiB before it left the host.
// Length is the prompt's length as typed, Redacted how many credentials were replaced, and Source how it was entered (typed,
// queued, an accepted suggestion). PromptID names the turn, which the session's tool calls made in it name too.
type AgentPrompt struct {
	PromptID  string `json:"prompt_id"`
	Text      string `json:"text"`
	Length    int64  `json:"length"`
	Truncated bool   `json:"truncated,omitempty"`
	Redacted  int64  `json:"redacted,omitempty"`
	Source    string `json:"source,omitempty"`
	AtNs      int64  `json:"at_ns"`
	// UID is the uid of the transcript the prompt was read from.
	UID int64 `json:"uid"`
}

// AgentPromptPage is one page of a session's prompts and the cursor for the next, empty on the last.
type AgentPromptPage struct {
	Prompts    []AgentPrompt `json:"prompts"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

// AgentToolCallFilter selects one session's tool calls on one host. FromNs/ToNs bound the time the agent recorded each call, zero
// meaning unbounded on that side; Limit caps the calls listed.
type AgentToolCallFilter struct {
	HostID    string
	SessionID string
	FromNs    int64
	ToNs      int64
	Limit     int
}

// AgentSessionLineage is a session's place among the sessions forked from each other on one host (issue #1347). A forked session
// begins with a copy of its parent's history, and each copied call names the session it was written under (origin_session_id): a
// call that the named session also holds, by tool_use_id and owner, is a copy of it. A session's parent is the origin of its latest
// copy, so a fork of a fork names the session it was forked from, not the first in the chain. ForkedSessionIDs are the sessions whose
// parent this session is, sorted.
//
// The copies are the only evidence: a fork of a fork that made no calls of its own before it was forked holds copies naming the
// session before it only, so it is read as that session's fork, and a fork made after /clear holds copies of the cleared session's
// calls naming the process's first session, which never held them, so they are read as the fork's own.
type AgentSessionLineage struct {
	ForkedFromSessionID string   `json:"forked_from_session_id,omitempty"`
	ForkedSessionIDs    []string `json:"forked_session_ids"`
}

// AgentToolCall is one tool call an AI agent session recorded in its transcript: the tool, a Bash call's command and the description
// the agent gave it, or a file tool's path, and when the agent made the call.
type AgentToolCall struct {
	ToolUseID   string `json:"tool_use_id"`
	ToolName    string `json:"tool_name"`
	Command     string `json:"command,omitempty"`
	Description string `json:"description,omitempty"`
	FilePath    string `json:"file_path,omitempty"`
	// Sidechain marks a call a subagent made, and Truncated a command cut to the agent's 8 KiB limit.
	Sidechain  bool  `json:"sidechain,omitempty"`
	Truncated  bool  `json:"truncated,omitempty"`
	CalledAtNs int64 `json:"called_at_ns"`
	// UID is the uid of the transcript the call was read from. A transcript is its user's to write, so a call is trusted only for the
	// processes of the user who owns it.
	UID int64 `json:"uid"`
}
