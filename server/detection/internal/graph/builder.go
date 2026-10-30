package graph

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/internal/mysql"
)

// pendingExitTTL caps how long an exit event hangs in the pending-exit buffer waiting for its companion snapshot exec (issue #176).
// 30s comfortably absorbs the worst observed post-restart agent reconnect + snapshot-batch upload window (~1-3s in dev QA), with a
// 10x safety margin. Beyond 30s we'd risk binding a stale exit to a freshly-recycled PID.
const pendingExitTTL = 30 * time.Second

// pendingExitKey identifies the (host, pid) tuple a buffered exit waits for. Snapshot exec ingest joins on this key.
type pendingExitKey struct {
	hostID string
	pid    int
}

// pendingExit captures everything needed to mark a snapshot row exited at insert time. Issue #176: when a NOTIFY_EXIT for a PID
// lands at the server before the snapshot exec for the same PID (small race after extension restart), the exit's UPDATE is a no-op
// (no row yet). We buffer the exit here for pendingExitTTL; the snapshot exec consumes it on arrival so we don't end up with a
// phantom alive row.
type pendingExit struct {
	exitTimeNs       int64
	exitIngestedAtNs int64
	exitCode         int
	exitReason       string
	// pidVersion is the exiting generation, when the exit carried it. A snapshot exec of a newer generation does not take the exit.
	pidVersion *uint32
	expiresAt  time.Time
}

// Builder incrementally materializes fork/exec/exit events into the
// processes table.
type Builder struct {
	store  *mysql.Store
	logger *slog.Logger
	now    func() time.Time

	pendingExitsMu sync.Mutex
	pendingExits   map[pendingExitKey]pendingExit
}

// NewBuilder creates a graph builder backed by the given store.
func NewBuilder(s *mysql.Store, logger *slog.Logger) *Builder {
	if logger == nil {
		logger = slog.Default()
	}
	return &Builder{
		store:        s,
		logger:       logger,
		now:          time.Now,
		pendingExits: make(map[pendingExitKey]pendingExit),
	}
}

// bufferPendingExit records an exit event whose UPDATE found no row, so a later snapshot exec for the same PID can carry the exit
// through to the synthesised row (issue #176). Overwrites any prior pending exit for the same (host, pid): the newest exit signal
// is authoritative since the previous one didn't bind either.
func (b *Builder) bufferPendingExit(hostID string, pid int, pe pendingExit) {
	pe.expiresAt = b.now().Add(pendingExitTTL)
	b.pendingExitsMu.Lock()
	b.pendingExits[pendingExitKey{hostID: hostID, pid: pid}] = pe
	b.pendingExitsMu.Unlock()
}

// consumePendingExit looks up a buffered exit for (hostID, pid). Returns the exit and removes it from the buffer when found and not
// expired. Returns ok=false when missing or expired. Expired entries are purged lazily here AND by sweepPendingExits at batch
// boundaries; both paths matter because not every snapshot exec arrives with the same lifecycle.
//
// pidversion and startTimeNs are the snapshot exec's generation and start time. An exit stamped before the process started, or from
// an older generation, is a process that ended before this one reused the pid, so it stays buffered rather than marking the newer
// process exited (issue #1280). The start time decides first because it holds across a reboot, where generations restart at zero.
func (b *Builder) consumePendingExit(hostID string, pid int, pidversion *uint32, startTimeNs *int64) (pendingExit, bool) {
	key := pendingExitKey{hostID: hostID, pid: pid}
	b.pendingExitsMu.Lock()
	defer b.pendingExitsMu.Unlock()
	pe, ok := b.pendingExits[key]
	if !ok {
		return pendingExit{}, false
	}
	if startTimeNs != nil && pe.exitTimeNs < *startTimeNs {
		return pendingExit{}, false
	}
	if pe.pidVersion != nil && pidversion != nil && *pe.pidVersion < *pidversion {
		return pendingExit{}, false
	}
	delete(b.pendingExits, key)
	if pe.expiresAt.Before(b.now()) {
		return pendingExit{}, false
	}
	return pe, true
}

// sweepPendingExits drops expired entries. Called once per ProcessBatch to keep the map size bounded in steady state -
// pendingExitTTL is short and the map rarely has more than a handful of entries on a healthy host.
func (b *Builder) sweepPendingExits() {
	now := b.now()
	b.pendingExitsMu.Lock()
	defer b.pendingExitsMu.Unlock()
	for k, pe := range b.pendingExits {
		if pe.expiresAt.Before(now) {
			delete(b.pendingExits, k)
		}
	}
}

// ProcessBatch materialises a batch of fork/exec/exit/snapshot-heartbeat events into the processes table, and records the agent
// sessions agent_usage events name in the bypass mode; other event types are ignored. It collapses what used to be one-or-more DB
// round-trips per event into a small constant (issue #535): the candidate process rows for every (host_id, pid) the batch touches
// are preloaded in one query into an in-memory session, the handlers fold the batch against that overlay (reproducing the exact
// per-event resolution semantics), and the result is persisted with set-based writes in one flush. The resulting forest is identical to applying the events one at a time in timestamp order.
func (b *Builder) ProcessBatch(ctx context.Context, events []api.Event) error {
	// Sort by timestamp so fork always precedes exec/exit for the same PID.
	sorted := make([]api.Event, len(events))
	copy(sorted, events)
	slices.SortStableFunc(sorted, func(a, be api.Event) int {
		if a.TimestampNs < be.TimestampNs {
			return -1
		}
		if a.TimestampNs > be.TimestampNs {
			return 1
		}
		return 0
	})

	// Sweep expired pending exits once per batch (issue #176 buffer hygiene). Bounded-size
	// map, called per batch is plenty.
	b.sweepPendingExits()

	sess, err := newBatchSession(ctx, b.store, collectKeys(sorted))
	if err != nil {
		return fmt.Errorf("graph: preload batch: %w", err)
	}

	var transientFailCount int
	for _, evt := range sorted {
		// Folding is in-memory: handler errors are payload-decode failures (permanent) only, since the session never touches the
		// DB here. A permanent data error (a value outside a column's range, the issue #379 uid/gid-overflow class) is not caught
		// here; it surfaces at flush, where the per-row fallback isolates it. Returning a transient error here would make the
		// processor re-fetch the whole batch forever, wedging the pipeline fleet-wide on one poison event.
		if err := b.foldEvent(ctx, sess, evt); err != nil {
			if permanentError(err) {
				b.logger.WarnContext(ctx, "event dropped: permanent processing error", "event_id", evt.EventID, "type", evt.EventType, "err", err)
				continue
			}
			transientFailCount++
			b.logger.WarnContext(ctx, "event processing failed, will retry", "event_id", evt.EventID, "type", evt.EventType, "err", err)
		}
	}
	if transientFailCount > 0 {
		return fmt.Errorf("graph: %d event(s) failed to process", transientFailCount)
	}

	// One set-based flush. A transient write fault is returned so the processor nacks and retries the whole batch atomically (the
	// flush rolls back on failure); a permanent data error is isolated inside the flush via a per-row fallback, dropping the
	// poison row and persisting the rest, so the batch still advances (issue #379).
	if err := b.store.FlushProcessBatch(ctx, sess.plan()); err != nil {
		return fmt.Errorf("graph: flush batch: %w", err)
	}
	return nil
}

// foldEvent applies one event against the writer (the in-memory session in production, the store directly in the differential
// reference test). Unhandled event types are a no-op.
func (b *Builder) foldEvent(ctx context.Context, w processStore, evt api.Event) error {
	switch evt.EventType {
	case "fork":
		return b.handleFork(ctx, w, evt)
	case "exec":
		return b.handleExec(ctx, w, evt)
	case "exit":
		return b.handleExit(ctx, w, evt)
	case "snapshot_heartbeat":
		return b.handleSnapshotHeartbeat(ctx, w, evt)
	case "agent_usage":
		return handleAgentUsage(ctx, w, evt)
	}
	return nil
}

// bypassPermissionsMode is the permission mode Claude Code records when it runs every tool call without asking.
const bypassPermissionsMode = "bypassPermissions"

type agentUsagePayload struct {
	AgentSessionID string `json:"agent_session_id"`
	PermissionMode string `json:"permission_mode"`
}

// handleAgentUsage records that the session the event names ran in the bypassPermissions mode (issue #1427). A forked session's copy
// of its parent's line names the fork, so a fork of a bypassed session is recorded too.
func handleAgentUsage(ctx context.Context, w processStore, evt api.Event) error {
	var p agentUsagePayload
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return err
	}
	if p.PermissionMode != bypassPermissionsMode {
		return nil
	}
	return w.ObserveBypass(ctx, mysql.BypassSighting{HostID: evt.HostID, SessionID: p.AgentSessionID, FirstSeenNs: evt.TimestampNs})
}

// permanentError reports whether err is a deterministic per-event failure that recurs on every retry: a payload that cannot be parsed
// (a JSON syntax or type error, including a number that overflows the target Go type) or a value the database can never store
// (mysql.IsPermanentDataError). The builder drops such an event so one poison row cannot wedge the batch; any other error is treated
// as a transient fault and the batch is retried. See issue #379.
func permanentError(err error) bool {
	var (
		syntaxErr *json.SyntaxError
		typeErr   *json.UnmarshalTypeError
	)
	if errors.As(err, &syntaxErr) || errors.As(err, &typeErr) {
		return true
	}
	return mysql.IsPermanentDataError(err)
}

type snapshotHeartbeatPayload struct {
	PID int `json:"pid"`
}

// handleSnapshotHeartbeat bumps last_seen_ns on the snapshot row for the heartbeat PID so the TTL reconciler's
// COALESCE(last_seen_ns, fork_time_ns) predicate sees a fresh timestamp and exempts the row from the issue #6 force-exit. No-ops
// for non-snapshot rows and for snapshot rows that have already exited; the store's WHERE clause guards both. Issue #173.
func (b *Builder) handleSnapshotHeartbeat(ctx context.Context, w processStore, evt api.Event) error {
	var p snapshotHeartbeatPayload
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return err
	}
	return w.UpdateLastSeenForSnapshot(ctx, evt.HostID, p.PID, evt.TimestampNs)
}

type forkPayload struct {
	ChildPID  int `json:"child_pid"`
	ParentPID int `json:"parent_pid"`
	// PIDVersion is the CHILD process's kernel PID generation (audit_token_to_pidversion), when the agent provided it. Stored on
	// the new generation so network/DNS flows correlate to it by exact identity rather than a fork-to-exit window. Nil for
	// agents that predate this field (issue #403).
	PIDVersion *uint32 `json:"pidversion"`
	// ParentPIDVersion is the FORKING process's generation, which with ParentPID names the child's creator exactly. Later
	// reparenting cannot change it, unlike the ppid an exec reports. Nil for agents that predate it (issue #1277).
	ParentPIDVersion *uint32 `json:"parent_pidversion"`
}

func (b *Builder) handleFork(ctx context.Context, w processStore, evt api.Event) error {
	var p forkPayload
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return err
	}

	// Idempotent re-processing (migration 00011): if this fork already created its row (a batch nack from a retryable detection miss,
	// or a claim-lease-expiry re-offer, replays the event), skip the Close+Insert so the replay is a no-op rather than a duplicate
	// generation plus a phantom pid_reuse close of the first-pass row.
	applied, err := w.EventAlreadyApplied(ctx, evt.HostID, p.ChildPID, evt.EventID)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}

	// Handle PID reuse: close any existing non-exited record for this PID.
	if err := w.CloseStaleProcess(ctx, evt.HostID, p.ChildPID, evt.TimestampNs); err != nil {
		return err
	}

	// Inherit the parent's image for the new process (fork-without-exec case). Resolved at the fork's own timestamp, not at "now":
	// the parent pid may already have been recycled by a later generation whose fork was materialized first, and inheriting that
	// generation's image attributes the child to a binary its parent never ran (issue #714).
	//
	// The signing identity is inherited with the path, from that same image. A forked child runs its parent's binary until it
	// execs, so the identity is as much its own as the path is, and recording the path without it left every signature exclusion
	// missing a forked process while matching an exec'd one (issue #1123). An exec on this pid overwrites all of it.
	//
	// When the fork named its creator exactly, by (parent_pid, parent_pidversion), the image is that row's (issue #1298): exec bumps
	// the pidversion, so the row is the image the creator was running when it forked. Picking by time instead can disagree with it
	// once the parent's pid is reused, and a child linked to its real creator would inherit the pid's next process's binary.
	creator, err := resolveCreator(ctx, w, evt, p.ParentPID, p.ParentPIDVersion)
	if err != nil {
		return err
	}
	parentImage := creatorImage(creator, evt.TimestampNs)
	if creator == nil {
		if parentImage, err = w.GetParentImage(ctx, evt.HostID, p.ParentPID, evt.TimestampNs); err != nil {
			b.logger.WarnContext(ctx, "failed to get parent image", "host_id", evt.HostID, "parent_pid", p.ParentPID, "err", err)
		}
	}
	inherited := inheritFrom(creator)
	storyRoot := inherited.storyRoot
	if p.ParentPID == launchdPID {
		storyRoot = new(storyRootSelf)
	}

	forkIngested := evt.IngestedAtNs
	sourceEventID := evt.EventID
	_, err = w.InsertProcess(ctx, api.Process{
		HostID:           evt.HostID,
		PID:              p.ChildPID,
		PPID:             p.ParentPID,
		Path:             parentImage.Path,
		CodeSigning:      parentImage.CodeSigning,
		SHA256:           parentImage.SHA256,
		CDHash:           parentImage.CDHash,
		PIDVersion:       p.PIDVersion,
		ParentProcessID:  inherited.parentID,
		ParentPIDVersion: p.ParentPIDVersion,
		// A forked child runs with its creator's environment and in its tool call, so it belongs to the creator's agent session
		// (issue #1281), and to its story unless launchd started it.
		AgentSession:     inherited.session,
		StoryRootID:      storyRoot,
		ToolCallGroup:    inherited.toolCall,
		ForkTimeNs:       evt.TimestampNs,
		ForkIngestedAtNs: &forkIngested,
		SourceEventID:    &sourceEventID,
	})
	return err
}

// resolveCreator returns the row of the process (pid, pidversion) that created the event's process at the event's time, or nil when
// the agent sent no generation, in which case readers keep the ppid rule (issue #1280).
func resolveCreator(ctx context.Context, w processStore, evt api.Event, pid int, pidversion *uint32) (*api.Process, error) {
	if pidversion == nil {
		return nil, nil
	}
	return w.ResolveParent(ctx, evt.HostID, pid, *pidversion, evt.TimestampNs)
}

// creatorImage is the image a forked child inherits from its resolved creator: the creator row's binary, with its signing identity
// only if that identity was in force at the fork, by the rule the time-based lookup applies too. Zero without a creator.
func creatorImage(creator *api.Process, forkNs int64) mysql.ParentImage {
	if creator == nil {
		return mysql.ParentImage{}
	}
	image := mysql.ParentImage{Path: creator.Path, CodeSigning: creator.CodeSigning, SHA256: creator.SHA256, CDHash: creator.CDHash}
	return image.IdentityInForceAt(creator.ExecTimeNs, forkNs)
}

// launchdPID is launchd's pid. A process whose fork-time parent it is starts a story (issue #1281).
const launchdPID = 1

// inheritance is what a new row takes from the row of its creator: the link to it (issue #1280), and its agent session, story root
// and tool call (issue #1281). All unset without a creator.
type inheritance struct {
	parentID  *int64
	session   api.AgentSession
	storyRoot *int64
	toolCall  *int
}

func inheritFrom(creator *api.Process) inheritance {
	if creator == nil {
		return inheritance{}
	}
	id := creator.ID
	return inheritance{parentID: &id, session: creator.AgentSession, storyRoot: creator.StoryRootID, toolCall: creator.ToolCallGroup}
}

// toolCallOr returns inherited when the row already has a tool call. Otherwise it returns the exec's own process group when the
// exec is the agent's direct child, created by the process its CLAUDE_PID marker names, which is how a tool call starts (issue
// #1281). Claude Code leads each tool call's process group, and the group survives daemonizing, so the key holds for everything the
// call starts; inheriting it rather than reading each row's own group keeps it through setsid as well, including a re-exec of the
// tool call's own shell after it changed groups.
func (p execPayload) toolCallOr(creatorPID int, inherited *int) *int {
	if inherited != nil {
		return inherited
	}
	if agentPID, err := strconv.Atoi(p.Env[markerAgentPID]); err == nil && agentPID == creatorPID && p.GroupID != nil {
		return p.GroupID
	}
	return inherited
}

// The agent session markers the extension forwards from an exec's environment (issue #1278).
const (
	// markerSessionID is Claude Code's session identifier, present on the processes it runs for a session.
	markerSessionID = "CLAUDE_CODE_SESSION_ID"
	// markerAgentPID is the pid of the agent process itself, on the processes it runs.
	markerAgentPID = "CLAUDE_PID"
	// markerAgent is the agent's self-description, name_version_role with dashes for dots: claude-code_2-1-296_agent.
	markerAgent = "AI_AGENT"
	// maxMarkerLen is the longest marker value a row stores, the extension's own cap and the columns' width. A longer value from
	// another sender is ignored rather than failing the whole row at flush.
	maxMarkerLen = 256
	// claudeCodeAgent names the agent when only its session marker is present.
	claudeCodeAgent = "claude-code"
)

// sessionOr returns the agent session the exec's own markers name, or inherited when it carries no usable session marker (issue
// #1281). A marker replaces what the row inherited, which is how /clear, or an agent started inside another, begins its own session;
// its absence keeps the inherited one, which is how a child that sudo or env -i stripped of its environment stays attributed.
func (p execPayload) sessionOr(inherited api.AgentSession) api.AgentSession {
	if p.marked {
		return p.session
	}
	return inherited
}

// markedSession returns the agent session the exec's own markers name, and false when it carries no usable session marker.
func (p execPayload) markedSession() (api.AgentSession, bool) {
	id := p.Env[markerSessionID]
	if id == "" || len(id) > maxMarkerLen {
		return api.AgentSession{}, false
	}
	session := api.AgentSession{AgentSessionID: &id, AgentName: new(claudeCodeAgent)}
	if agent := p.Env[markerAgent]; agent != "" && len(agent) <= maxMarkerLen {
		parts := strings.SplitN(agent, "_", 3)
		session.AgentName = &parts[0]
		if len(parts) > 1 && parts[1] != "" {
			session.AgentVersion = new(strings.ReplaceAll(parts[1], "-", "."))
		}
	}
	return session, true
}

// observeSession records the exec as a sighting of the agent session its markers name, if any (issue #1281). The agent pid is
// kept only when it fits the column, since CLAUDE_PID is whatever the process environment says.
func (p execPayload) observeSession(ctx context.Context, w processStore, evt api.Event) error {
	if !p.marked {
		return nil
	}
	sighting := mysql.SessionSighting{HostID: evt.HostID, Session: p.session, CWD: p.lineage().CWD, FirstSeenNs: evt.TimestampNs,
		FirstEventID: evt.EventID, LastSeenNs: evt.TimestampNs}
	if pid, err := strconv.ParseInt(p.Env[markerAgentPID], 10, 32); err == nil {
		sighting.AgentPID = new(int(pid))
	}
	return w.ObserveSession(ctx, sighting)
}

type execPayload struct {
	PID         int             `json:"pid"`
	PPID        int             `json:"ppid"`
	Path        string          `json:"path"`
	Args        api.NullRawJSON `json:"args"`
	UID         *int            `json:"uid"`
	GID         *int            `json:"gid"`
	CodeSigning api.NullRawJSON `json:"code_signing"`
	SHA256      *string         `json:"sha256"`
	// CDHash is the 40-hex code-directory hash; agent emits it only for Hardened-Runtime binaries (issue #68 / PR #185). Decoder
	// tolerates absence: non-HR rows + pre-cdhash agents simply leave the field nil and the persisted column stays NULL.
	CDHash *string `json:"cdhash"`
	// PIDVersion is the exec'd process's kernel PID generation (audit_token_to_pidversion), when the agent provided it. Stored on
	// the process record so flows correlate by exact identity. The extension reads it from the exec TARGET's audit token, and
	// execve increments the generation, so this never matches what the fork stored for the same pid: it is the identity of the
	// image now running, which is also what the agent's kill-time registry holds. Nil for agents that predate this field
	// (issue #403).
	PIDVersion *uint32 `json:"pidversion"`
	// Env holds the AI agent session markers the extension read from the process environment, only its allowlisted names
	// (issue #1278). Absent for a process that carries none. Ingest stamps the agent session from it in #1281.
	Env map[string]string `json:"env,omitempty"`
	// ParentPIDVersion is the generation of the parent PPID names: the CURRENT parent, launchd (1) for a daemonized process, not
	// the creator. The two together identify that parent exactly (issue #1277).
	ParentPIDVersion *uint32 `json:"parent_pidversion,omitempty"`
	// ExecLineage carries the rest of the lineage fields the exec reports, under their wire names. Read it through lineage(),
	// which derives Reparented and drops an empty cwd.
	api.ExecLineage
	// Snapshot is true for synthetic exec events emitted by the ESF startup baseline pass (issue #11). The graph builder uses this
	// to avoid clobbering a richer live-event row with a sparse synthetic one when the snapshot pass and an early-startup live exec
	// both arrive for the same PID.
	Snapshot bool `json:"snapshot"`

	// session is the agent session the exec's own markers name, parsed once by handleExec; marked is false when it carries none.
	session api.AgentSession
	marked  bool
}

// lineage is the exec's lineage as the row stores it (issue #1280). Reparented is derived here rather than trusted from the wire: the
// process was adopted by launchd when its current parent is 1 and its creator was another process. An empty cwd, which agents that
// predate the field send, is stored as unknown.
func (p execPayload) lineage() api.ExecLineage {
	l := p.ExecLineage
	l.Reparented = p.PPID == 1 && l.OriginalPPID != nil && *l.OriginalPPID > 1
	if l.CWD != nil && *l.CWD == "" {
		l.CWD = nil
	}
	return l
}

func (b *Builder) handleExec(ctx context.Context, w processStore, evt api.Event) error {
	var p execPayload
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return err
	}
	p.session, p.marked = p.markedSession()

	// Idempotent re-processing (migration 00011): if this exec already created or imaged the row (a batch nack from a retryable
	// detection miss, or a claim-lease-expiry re-offer, replays it), skip. Without this, a re-applied first-exec-after-fork (whose
	// case-b UPDATE already set exec_time_ns) would be re-read below as a case-c re-exec and fabricate a phantom generation.
	applied, err := w.EventAlreadyApplied(ctx, evt.HostID, p.PID, evt.EventID)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}
	if err := p.observeSession(ctx, w, evt); err != nil {
		return err
	}

	// Resolve the running-at-this-moment row for the PID. Three shapes drive different recovery paths:
	//   (a) no row: exec-without-fork (synthesize a root row).
	//   (b) row, exec_time_ns NULL: first exec after fork; UPDATE in place.
	//   (c) row, exec_time_ns set: same-PID re-exec (issue #10). Close the prior generation and INSERT a new linked row.
	//       Without this branch, shell exec-optimization chains (python -> sh -> bash -> /tmp/payload) get collapsed
	//       into the final exec only.
	current, err := w.GetProcessByPID(ctx, evt.HostID, p.PID, evt.TimestampNs)
	if err != nil {
		return err
	}

	// Snapshot dedup: a snapshot exec for a PID that already has a fully-materialised live row would otherwise hit the re-exec branch
	// below and replace the live row (real args, code_signing, sha256) with a sparse snapshot row. The race window is small but real.
	// See issue #11 review: extension's snapshot pass fires ~ms after es_subscribe, and any process the live stream observed in
	// that window is in `processes` before the snapshot batch ingests. Drop the snapshot exec; the live data is the authoritative one.
	//
	// The one exception is a row an earlier extension's snapshot left without a signature, which a later snapshot of the same process
	// completes: the process is the same one when the row is still open and its path matches, and only a missing signature is
	// written. A live exec always carries a signature, so nothing a live exec recorded is replaced. The match is by pid and path, with no
	// start time: if the row's process exited unseen and the same program reused its pid, the row already stands for the process now
	// running (this dedup drops that process's own snapshot), so the signature read from it is the right one for what the row judges.
	if p.Snapshot && current != nil && current.ExecTimeNs != nil {
		if len(current.CodeSigning) == 0 && len(p.CodeSigning) > 0 && current.Path == p.Path {
			return w.BackfillSnapshotSigning(ctx, evt.HostID, p.PID, current.ID, p.CodeSigning)
		}
		return nil
	}

	if current == nil {
		return b.insertExecWithoutFork(ctx, w, evt, p)
	}
	if current.ExecTimeNs == nil {
		execEventID := evt.EventID
		return w.UpdateProcessExec(ctx, mysql.ProcessExecUpdate{
			HostID: evt.HostID, PID: p.PID, ExecTimeNs: evt.TimestampNs,
			Path: p.Path, Args: p.Args,
			UID: p.UID, GID: p.GID,
			CodeSigning: p.CodeSigning, SHA256: p.SHA256, CDHash: p.CDHash,
			PIDVersion:  p.PIDVersion,
			ExecEventID: &execEventID,
			Lineage:     p.lineage(),
			Agent:       p.sessionOr(current.AgentSession),
			// The row's creator is the ppid its fork recorded.
			ToolCallGroup: p.toolCallOr(current.PPID, current.ToolCallGroup),
		})
	}
	return b.insertReExec(ctx, w, evt, p, current)
}

// insertExecWithoutFork synthesizes a root process row when an exec event arrives for a PID we've never seen fork for. fork_time_ns is
// set to the exec time as a best effort.
func (b *Builder) insertExecWithoutFork(ctx context.Context, w processStore, evt api.Event, p execPayload) error {
	ppid := 0
	if p.PPID != 0 {
		ppid = p.PPID
	}
	lineage := p.lineage()
	// With no fork seen, the creator is known only when the parent the exec reports is still it. A reparented process names launchd
	// there, and its creator's generation is not on the exec, so the link stays unset rather than guessed from a pid.
	var creator *api.Process
	if !lineage.Reparented {
		var err error
		if creator, err = resolveCreator(ctx, w, evt, p.PPID, p.ParentPIDVersion); err != nil {
			return err
		}
	}
	inherited := inheritFrom(creator)
	storyRoot := inherited.storyRoot
	// Launchd started it only when the exec says so: a startup snapshot reports no original_ppid, and a daemon launchd merely
	// adopted would otherwise be taken for the start of a story.
	if lineage.OriginalPPID != nil && *lineage.OriginalPPID == launchdPID {
		storyRoot = new(storyRootSelf)
	}
	forkIngested := evt.IngestedAtNs
	sourceEventID := evt.EventID
	row := api.Process{
		HostID:      evt.HostID,
		PID:         p.PID,
		PPID:        ppid,
		Path:        p.Path,
		Args:        p.Args,
		UID:         p.UID,
		GID:         p.GID,
		CodeSigning: p.CodeSigning,
		SHA256:      p.SHA256,
		CDHash:      p.CDHash,
		PIDVersion:  p.PIDVersion,
		// With no fork seen, the parent this row can name is the one the exec reports, so ppid and its generation stay a pair.
		ParentProcessID:  inherited.parentID,
		ParentPIDVersion: p.ParentPIDVersion,
		ExecLineage:      lineage,
		AgentSession:     p.sessionOr(inherited.session),
		StoryRootID:      storyRoot,
		ToolCallGroup:    p.toolCallOr(p.PPID, inherited.toolCall),
		ForkTimeNs:       evt.TimestampNs,
		ForkIngestedAtNs: &forkIngested,
		ExecTimeNs:       &evt.TimestampNs,
		IsSnapshot:       p.Snapshot,
		// The synthesised root is created and imaged by this one exec event; stamping both ids makes a re-processed exec-without-fork
		// a no-op (migration 00011) rather than a duplicate generation.
		SourceEventID: &sourceEventID,
		ExecEventID:   &sourceEventID,
	}
	// Issue #173: snapshot rows seed last_seen_ns at insert time so the TTL reconciler's COALESCE(last_seen_ns, fork_time_ns)
	// predicate gives them a fresh baseline. Without this, the very first reconciliation pass after a snapshot insert would see
	// last_seen_ns=NULL -> fall back to fork_time_ns -> indistinguishable from a stale row.
	if p.Snapshot {
		seen := evt.TimestampNs
		row.LastSeenNs = &seen
	}
	// Issue #176: if a NOTIFY_EXIT for this PID arrived ahead of the snapshot exec, the
	// graph builder buffered it in pendingExits. Consume the buffer now so the synthesised
	// row is born exited rather than a phantom alive row that survives until TTL.
	//
	// Lifetime quirk: the buffered exit's timestamp is necessarily BEFORE the snapshot
	// exec event timestamp (the kernel observed the exit first; the snapshot enumeration
	// just happened to emit later). Inserting fork_time_ns = exec_time, exit_time_ns =
	// buffered exit_time would produce a row with fork > exit, which GetProcessByPID's
	// "alive at atTimeNs" predicate rejects entirely. Pull fork_time_ns back to the exit
	// time so the row is born already-exited with a zero-length lifetime: best we can
	// do given the snapshot exec carries no real fork time. last_seen_ns mirrors the same
	// timestamp so the row doesn't look "fresh" to the TTL reconciler.
	if p.Snapshot {
		if pe, ok := b.consumePendingExit(evt.HostID, p.PID, p.PIDVersion, p.StartTimeNs); ok {
			row.ForkTimeNs = pe.exitTimeNs
			row.ExecTimeNs = &pe.exitTimeNs
			row.LastSeenNs = &pe.exitTimeNs
			row.ExitTimeNs = &pe.exitTimeNs
			row.ExitIngestedAtNs = &pe.exitIngestedAtNs
			reason := pe.exitReason
			row.ExitReason = &reason
			row.ExitCode = &pe.exitCode
		}
	}
	_, err := w.InsertProcess(ctx, row)
	return err
}

// insertReExec handles the issue #10 branch: a process called execve() again on the same PID without forking in between. The store's
// ReExec helper wraps the close + insert in a single transaction so we can never leave the PID appearing exited with no current
// generation (partial failure).
func (b *Builder) insertReExec(ctx context.Context, w processStore, evt api.Event, p execPayload, prior *api.Process) error {
	sourceEventID := evt.EventID
	_, reLinked, err := w.ReExec(ctx, prior.ID, evt.TimestampNs, evt.IngestedAtNs, api.Process{
		HostID: evt.HostID,
		PID:    p.PID,
		// Preserve the parent linkage from the original fork: a re-exec doesn't change PPID on macOS. Falls back to whatever
		// the exec event carries if the prior row somehow has ppid=0.
		PPID:        pickPPID(prior.PPID, p.PPID),
		Path:        p.Path,
		Args:        p.Args,
		UID:         p.UID,
		GID:         p.GID,
		CodeSigning: p.CodeSigning,
		SHA256:      p.SHA256,
		CDHash:      p.CDHash,
		// Prefer the exec event's pidversion over the prior generation's: execve increments the kernel PID generation, so the
		// re-exec'd image has an identity the closed generation does not share, and persisting the closed one gets a kill on this
		// image refused as a generation mismatch. When the event reports none, keep the prior generation's value rather than
		// nulling: the agent's registry derives its own value from this same event stream (procgen only records a generation for
		// an envelope that carries one), so both sides keep the prior value and stay consistent, and the pin still catches PID
		// reuse because a recycled pid arrives with a distant generation. Nulling would instead drop the pin entirely, leaving the
		// UI to send a pid-only kill the agent does not check at all. Same rule as the first-exec-after-fork path, which spells it
		// COALESCE(?, pidversion) in SQL and mirrors it in the batch session.
		PIDVersion: cmp.Or(p.PIDVersion, prior.PIDVersion),
		// The parent link belongs to the process, not the image, so the new generation keeps the one its fork recorded, the same
		// way it keeps PPID. The rest of the lineage is what this exec reports.
		ParentProcessID:  prior.ParentProcessID,
		ParentPIDVersion: prior.ParentPIDVersion,
		ExecLineage:      p.lineage(),
		// An exec keeps the process's environment unless it was given a new one, so the session carries over the same way.
		AgentSession:     p.sessionOr(prior.AgentSession),
		StoryRootID:      prior.StoryRootID,
		ToolCallGroup:    p.toolCallOr(prior.PPID, prior.ToolCallGroup),
		ForkTimeNs:       prior.ForkTimeNs, // chain preserves the original fork time
		ForkIngestedAtNs: prior.ForkIngestedAtNs,
		ExecTimeNs:       &evt.TimestampNs,
		// This exec event both created the generation and applied its image; stamping both ids makes a re-processed exec a no-op
		// (migration 00011) instead of fabricating another re-exec generation.
		SourceEventID: &sourceEventID,
		ExecEventID:   &sourceEventID,
	})
	if err != nil {
		return fmt.Errorf("re-exec prior id=%d: %w", prior.ID, err)
	}
	if !reLinked {
		// Prior row was terminally closed (observed exit or pid reuse):
		// we anchored a fresh chain instead of chaining through.
		b.logger.WarnContext(ctx, "re-exec arrived after prior generation was terminally closed",
			"host_id", evt.HostID, "pid", p.PID, "prior_row_id", prior.ID)
	}
	return nil
}

// pickPPID prefers a non-zero prior PPID. macOS ES reports PPID on every NOTIFY_EXEC and it should be identical for re-execs on the
// same pid, but staying defensive: if prior has it and the event doesn't, use prior.
func pickPPID(prior, fromEvent int) int {
	if prior != 0 {
		return prior
	}
	return fromEvent
}

type exitPayload struct {
	PID int `json:"pid"`
	// PIDVersion is the exiting process's kernel PID generation, when the agent provided it (issue #1279). It binds the exit to
	// that generation's row, never a newer one (issue #1280).
	PIDVersion *uint32 `json:"pidversion,omitempty"`
	ExitCode   int     `json:"exit_code"`
	ExitReason string  `json:"exit_reason,omitempty"`
}

func (b *Builder) handleExit(ctx context.Context, w processStore, evt api.Event) error {
	var p exitPayload
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return err
	}

	// Idempotent re-processing (migration 00011): the exit close is an in-place UPDATE of the most-recent-live row, so a replayed exit
	// (a batch nack from a retryable detection miss, or a claim-lease-expiry re-offer) would otherwise close a NEWER generation of a
	// reused pid. If this exit already closed a row for the pid, the replay is a no-op.
	applied, err := w.EventAlreadyApplied(ctx, evt.HostID, p.PID, evt.EventID)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}

	// Whitelist: only the agent-emitted reconciled reason is honoured on the wire. Anything else (server-only synthetic reasons: reexec,
	// pid_reuse, ttl_reconciliation) collapses to ExitReasonEvent so a compromised agent can't mark normal exits with a misleading reason.
	reason := api.ExitReasonEvent
	if p.ExitReason == api.ExitReasonHostReconciled {
		reason = api.ExitReasonHostReconciled
	}

	affected, err := w.UpdateProcessExit(ctx, mysql.ProcessExitUpdate{
		HostID:           evt.HostID,
		PID:              p.PID,
		ExitTimeNs:       evt.TimestampNs,
		ExitIngestedAtNs: evt.IngestedAtNs,
		ExitCode:         p.ExitCode,
		Reason:           reason,
		ExitEventID:      evt.EventID,
		PIDVersion:       p.PIDVersion,
	})
	if err != nil {
		return err
	}
	if affected == 0 {
		// Issue #176: the genuine no-row case is the post-restart race where the extension's snapshot enumerator saw the PID but
		// emitted its synthetic exec AFTER the live NOTIFY_EXIT landed. Buffer the exit so the inbound snapshot exec can consume it and
		// synthesise an already-exited row, rather than a phantom alive row that survives until 6h TTL reconciliation. Reason is
		// preserved so the synthesised row's exit_reason reflects host_reconciled vs event correctly.
		//
		// UpdateProcessExit can now also affect 0 rows via its fork_time_ns <= exit bound (migration 00011): on re-processing, the only
		// live row for the pid may have forked AFTER this exit, so the exit is stale for the current generation and must NOT be buffered
		// (a later snapshot exec would wrongly consume it and mark that row exited). Buffer only when no live row exists for the pid at
		// all, i.e. the real exit-before-fork race, not the fork-time-bound no-op.
		live, err := w.GetProcessByPID(ctx, evt.HostID, p.PID, math.MaxInt64)
		if err != nil {
			return err
		}
		if live == nil {
			b.bufferPendingExit(evt.HostID, p.PID, pendingExit{
				exitTimeNs:       evt.TimestampNs,
				exitIngestedAtNs: evt.IngestedAtNs,
				exitCode:         p.ExitCode,
				exitReason:       reason,
				pidVersion:       p.PIDVersion,
			})
		}
	}
	return nil
}
