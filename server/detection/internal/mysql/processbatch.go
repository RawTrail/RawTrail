package mysql

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/jmoiron/sqlx"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

// processBatchSelectColumns is the full process-row projection, shared by the bulk preload here and the single-row reads in
// processes.go so the in-memory batch overlay sees byte-identical rows to the per-event path. It is processReadColumns plus the
// event ids the builder's idempotency guard reads (migration 00011).
const processBatchSelectColumns = processReadColumns + `, source_event_id, exec_event_id, exit_event_id`

// processReadColumns is the projection the tree and process-detail reads return: every column a reader shows, including the lineage
// (issue #1280) the tree links by.
const processReadColumns = `id, host_id, pid, ppid, path, args, uid, gid, code_signing, sha256, cdhash, pidversion,
	fork_time_ns, fork_ingested_at_ns, exec_time_ns, exit_time_ns,
	exit_ingested_at_ns, exit_reason, exit_code, previous_exec_id,
	is_snapshot, last_seen_ns, ` + processLineageColumns + `, ` + agentSessionColumns + `, ` + groupingColumns

// loadKeyChunk caps the (host_id, pid) pairs folded into one preload statement. The row-constructor IN list binds two
// placeholders per key, so 1000 keys stay well under MySQL's placeholder ceiling while collapsing the per-event SELECTs into a
// handful of statements.
const loadKeyChunk = 1000

// processUpdateChunk caps the rows folded into one set-based UPDATE. Each row binds an id and a value for each of the 29 CASE
// columns plus one IN-list id (59 placeholders), so a few hundred per chunk keeps the statement well bounded.
const processUpdateChunk = 200

// insertRowChunk caps the rows folded into one multi-row INSERT. Each row binds 40 placeholders, so this keeps the statement under
// MySQL's ~65k placeholder ceiling even if the configured processing batch grows well beyond today's default; the preload and
// update paths are chunked for the same reason.
const insertRowChunk = 1000

// HostPID identifies a process lineage key for the batch preload.
type HostPID struct {
	HostID string
	PID    int
}

// LoadProcessesForKeys returns every process row for the given (host_id, pid) pairs, ordered oldest-first by (fork_time_ns, id).
// The graph builder's batch path preloads the full candidate set in one round-trip and then resolves every per-event read
// (GetProcessByPID, GetParentImage) against the in-memory overlay instead of issuing a SELECT per event. All rows for each key are
// returned, including already-exited ones, because every overlay read is bracketed against the READING EVENT's timestamp rather
// than against now, and a batch routinely carries events older than the newest persisted generation of a pid: a generation that has
// since exited is still the right answer for an event stamped inside its lifetime, and for a fork's inherited path it stays a
// candidate whatever its exit says. The per-(host_id, pid) row count is bounded by the retention prune, so this stays small in
// steady state.
func (s *Store) LoadProcessesForKeys(ctx context.Context, keys []HostPID) ([]api.Process, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	var out []api.Process
	for start := 0; start < len(keys); start += loadKeyChunk {
		end := min(start+loadKeyChunk, len(keys))
		chunk, err := s.loadProcessesChunk(ctx, keys[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, chunk...)
	}
	return out, nil
}

func (s *Store) loadProcessesChunk(ctx context.Context, keys []HostPID) ([]api.Process, error) {
	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(processBatchSelectColumns)
	sb.WriteString(" FROM processes WHERE (host_id, pid) IN (")
	args := make([]any, 0, len(keys)*2)
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("(?, ?)")
		args = append(args, k.HostID, k.PID)
	}
	sb.WriteString(") ORDER BY fork_time_ns, id")
	var procs []api.Process
	if err := s.db.SelectContext(ctx, &procs, sb.String(), args...); err != nil {
		return nil, fmt.Errorf("load processes for %d keys: %w", len(keys), err)
	}
	return procs, nil
}

// NewProcessRow is one row the batch fold wants inserted. Proc carries the final field values. PrevNewIndex links a re-exec child
// to a prior generation that was ALSO created in this batch (an index into the plan's NewRows slice); it is -1 when there is no
// same-batch predecessor, in which case Proc.PreviousExecID already holds the real id of a preloaded predecessor (or nil). The
// split exists because a same-batch predecessor has no id until it is inserted, so the flush resolves the link after assigning ids.
//
// ParentNewIndex does the same for the creator link (issue #1280): the index of a row created in this batch that Proc's creator
// resolved to, with Proc.ParentProcessID nil, or -1 when ParentProcessID already holds a real id (or nil). StoryRootNewIndex does it
// for the story root (issue #1281), and is the row's own index when the row is its own root.
type NewProcessRow struct {
	Proc              api.Process
	PrevNewIndex      int
	ParentNewIndex    int
	StoryRootNewIndex int
}

// ProcessRowUpdate is the final state of a preloaded row the batch fold mutated (an exit, an exec patch, a PID-reuse close, a
// re-exec close, or a freshness bump). The flush writes the full mutable column set to the row's final value, so a column the fold
// left untouched is rewritten to its existing value (a no-op) rather than tracked per-column. Immutable columns (host_id, pid,
// ppid, fork_time_ns, fork_ingested_at_ns, previous_exec_id, is_snapshot, parent_process_id, parent_pidversion, story_root_id) are never in this
// set.
type ProcessRowUpdate struct {
	ID               int64
	Path             string
	Args             api.NullRawJSON
	UID              *int
	GID              *int
	CodeSigning      api.NullRawJSON
	SHA256           *string
	CDHash           *string
	ExecTimeNs       *int64
	PIDVersion       *uint32
	ExecEventID      *string
	ExitTimeNs       *int64
	ExitIngestedAtNs *int64
	ExitReason       *string
	ExitCode         *int
	ExitEventID      *string
	LastSeenNs       *int64
	// Lineage is the exec-reported lineage (issue #1280), which a first exec after fork writes onto the fork's row.
	Lineage api.ExecLineage
	// Agent is the agent session (issue #1281), which an exec's markers replace.
	Agent api.AgentSession
	// ToolCallGroup is the tool-call key (issue #1281), which an exec by the agent's direct child sets. story_root_id is fixed at
	// insert, like the parent link, so it is not here.
	ToolCallGroup *int
}

// ProcessBatchPlan is the set of writes the graph builder's in-memory fold produced for one batch: new rows to insert (in creation
// order) and preloaded rows to update. The plan is pure data so the builder stays free of SQL and the fold is unit-testable.
type ProcessBatchPlan struct {
	NewRows          []NewProcessRow
	Updates          []ProcessRowUpdate
	SigningBackfills []SigningBackfill
	Sessions         []SessionSighting
	Bypass           []BypassSighting
}

// SessionSighting is what one batch saw of an AI agent session on a host (issue #1281): its identity and what its earliest sighting
// in the batch reported, with the span of the sightings. The flush folds it into agent_sessions, where first and last seen only move
// outward and the agent pid and cwd come from the earliest sighting overall.
type SessionSighting struct {
	HostID      string
	Session     api.AgentSession
	AgentPID    *int
	CWD         *string
	FirstSeenNs int64
	// FirstEventID is the earliest sighting's event, which breaks a tie between sightings at the same FirstSeenNs.
	FirstEventID string
	LastSeenNs   int64
}

// BypassSighting is the earliest event in one batch that named an agent session on a host in the bypassPermissions mode (issue
// #1427). The flush folds it into agent_session_bypass, whose first seen only moves earlier.
type BypassSighting struct {
	HostID      string
	SessionID   string
	FirstSeenNs int64
}

// SigningBackfill completes the code signature of a preloaded row an earlier extension's snapshot left unsigned. It is written as its
// own conditional UPDATE rather than folded into ProcessRowUpdate, whose full-row write would carry this batch's stale view of
// last_seen_ns and the exit columns over a heartbeat or a TTL exit another writer committed after the preload.
type SigningBackfill struct {
	ID          int64
	CodeSigning api.NullRawJSON
}

// Empty reports whether the plan would issue no writes.
func (p ProcessBatchPlan) Empty() bool {
	return len(p.NewRows) == 0 && len(p.Updates) == 0 && len(p.SigningBackfills) == 0 && len(p.Sessions) == 0 && len(p.Bypass) == 0
}

// backfillSigningGuard limits a signature backfill to a row that is still open and still has none, so a concurrent exit or an
// earlier backfill wins.
const backfillSigningGuard = `code_signing IS NULL AND exit_time_ns IS NULL`

// backfillSigning writes the backfills in chunks of processUpdateChunk, each one set-based UPDATE keyed by id: an upgraded endpoint's
// first snapshot can complete hundreds of rows in one batch, which per-row statements would turn into hundreds of round trips.
func backfillSigning(ctx context.Context, ext sqlx.ExtContext, backfills []SigningBackfill) error {
	for start := 0; start < len(backfills); start += processUpdateChunk {
		chunk := backfills[start:min(start+processUpdateChunk, len(backfills))]
		var sb strings.Builder
		args := make([]any, 0, len(chunk)*3)
		sb.WriteString("UPDATE processes SET code_signing = CASE id")
		for _, b := range chunk {
			sb.WriteString(" WHEN ? THEN ?")
			args = append(args, b.ID, b.CodeSigning)
		}
		sb.WriteString(" END WHERE id IN (?" + strings.Repeat(", ?", len(chunk)-1) + ") AND " + backfillSigningGuard)
		for _, b := range chunk {
			args = append(args, b.ID)
		}
		if _, err := ext.ExecContext(ctx, sb.String(), args...); err != nil {
			return fmt.Errorf("backfill code_signing for %d processes: %w", len(chunk), err)
		}
	}
	return nil
}

// upsertSessions folds the batch's session sightings into agent_sessions, in chunks of processUpdateChunk rows. The new row's values
// win the pid, cwd, name and version only when it was seen earlier than the stored row, comparing (first_seen_ns, first_event_id) so
// a tie at the same nanosecond resolves the same way whatever arrived first, and those assignments come before the first-seen pair's
// own, because MySQL applies an ON DUPLICATE KEY UPDATE list left to right. Keys arrive sorted, so two batches
// touching the same sessions lock them in the same order.
func upsertSessions(ctx context.Context, ext sqlx.ExtContext, sessions []SessionSighting) error {
	const earlier = `IF((new.first_seen_ns, new.first_event_id) < (agent_sessions.first_seen_ns, agent_sessions.first_event_id),
		new.%[1]s, agent_sessions.%[1]s)`
	var update strings.Builder
	for _, col := range []string{"agent_name", "agent_version", "agent_pid", "cwd", "first_event_id", "first_seen_ns"} {
		update.WriteString(col + " = " + fmt.Sprintf(earlier, col) + ", ")
	}
	update.WriteString(`last_seen_ns = GREATEST(agent_sessions.last_seen_ns, new.last_seen_ns)`)
	for start := 0; start < len(sessions); start += processUpdateChunk {
		chunk := sessions[start:min(start+processUpdateChunk, len(sessions))]
		args := make([]any, 0, len(chunk)*9)
		for _, s := range chunk {
			args = append(args, s.HostID, s.Session.AgentSessionID, s.Session.AgentName, s.Session.AgentVersion, s.AgentPID, s.CWD,
				s.FirstSeenNs, s.FirstEventID, s.LastSeenNs)
		}
		query := `INSERT INTO agent_sessions
			(host_id, agent_session_id, agent_name, agent_version, agent_pid, cwd, first_seen_ns, first_event_id, last_seen_ns)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)` + strings.Repeat(", (?, ?, ?, ?, ?, ?, ?, ?, ?)", len(chunk)-1) + `
			AS new ON DUPLICATE KEY UPDATE ` + update.String()
		if _, err := ext.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("upsert %d agent sessions: %w", len(chunk), err)
		}
	}
	return nil
}

// upsertBypass folds the batch's bypass sightings into agent_session_bypass, in chunks of processUpdateChunk rows, keeping each
// session's earliest. Keys arrive sorted, as upsertSessions' do.
func upsertBypass(ctx context.Context, ext sqlx.ExtContext, sightings []BypassSighting) error {
	for start := 0; start < len(sightings); start += processUpdateChunk {
		chunk := sightings[start:min(start+processUpdateChunk, len(sightings))]
		args := make([]any, 0, len(chunk)*3)
		for _, b := range chunk {
			args = append(args, b.HostID, b.SessionID, b.FirstSeenNs)
		}
		query := `INSERT INTO agent_session_bypass (host_id, agent_session_id, first_seen_ns)
			VALUES (?, ?, ?)` + strings.Repeat(", (?, ?, ?)", len(chunk)-1) + `
			AS new ON DUPLICATE KEY UPDATE first_seen_ns = LEAST(agent_session_bypass.first_seen_ns, new.first_seen_ns)`
		if _, err := ext.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("upsert %d agent session bypass sightings: %w", len(chunk), err)
		}
	}
	return nil
}

// FlushProcessBatch persists a batch plan. The fast path runs in one transaction: a single multi-row INSERT for the new rows (when
// none links to a same-batch predecessor) plus chunked set-based UPDATEs for the modified rows, collapsing what used to be
// ~1000+ per-event round-trips into a small constant.
//
// Poison isolation (issue #379) is preserved: if any batched write fails with a permanent data error (a value the database can
// never store), the whole fast path rolls back and the plan is re-applied row by row, dropping just the offending row(s) and
// committing the rest, so one poison row cannot wedge the batch. A transient (retryable) fault is returned so the caller nacks and
// the processor retries the whole batch, exactly as the per-event path did.
func (s *Store) FlushProcessBatch(ctx context.Context, plan ProcessBatchPlan) error {
	if plan.Empty() {
		return nil
	}
	err := s.flushProcessBatchFast(ctx, plan)
	if err == nil {
		return nil
	}
	if !IsPermanentDataError(err) {
		return err // transient fault: caller nacks and the batch is retried
	}
	// A row carries a value the DB can never store. Re-apply per row so the poison row is dropped and the rest persists.
	return s.flushProcessBatchPerRow(ctx, plan)
}

// flushProcessBatchFast applies the plan with set-based statements inside one transaction. Any error rolls the transaction back via
// the deferred Rollback; the caller classifies it (permanent -> per-row fallback, transient -> retry).
func (s *Store) flushProcessBatchFast(ctx context.Context, plan ProcessBatchPlan) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin process-batch tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if err := s.insertNewRowsBatched(ctx, tx, plan.NewRows); err != nil {
		return err
	}
	if err := s.updateRowsBatched(ctx, tx, plan.Updates); err != nil {
		return err
	}
	if err := backfillSigning(ctx, tx, plan.SigningBackfills); err != nil {
		return err
	}
	if err := upsertSessions(ctx, tx, plan.Sessions); err != nil {
		return err
	}
	if err := upsertBypass(ctx, tx, plan.Bypass); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit process-batch tx: %w", err)
	}
	return nil
}

// insertNewRowsBatched inserts every new row in a single multi-row INSERT when none links to a same-batch predecessor (the common
// case). When a same-batch re-exec link exists (a process forked, exec'd, and re-exec'd within one batch), the predecessor's id is
// not known until it is inserted, so the rows are inserted individually in creation order to resolve the linkage live.
func (s *Store) insertNewRowsBatched(ctx context.Context, ext sqlx.ExtContext, rows []NewProcessRow) error {
	if len(rows) == 0 {
		return nil
	}
	if hasSameBatchLink(rows) {
		return insertNewRowsLinked(ctx, ext, rows, false, s.logger)
	}
	for start := 0; start < len(rows); start += insertRowChunk {
		end := min(start+insertRowChunk, len(rows))
		if err := insertNewRowsChunk(ctx, ext, rows[start:end]); err != nil {
			return err
		}
	}
	ids, err := newRowIDs(ctx, ext, rows)
	if err != nil {
		return err
	}
	return linkSameBatchRows(ctx, ext, rows, ids)
}

// sameBatchLinked reports whether row links to a row created in the same batch by a column linkSameBatchRows writes.
func sameBatchLinked(r NewProcessRow) bool {
	return r.ParentNewIndex >= 0 || r.StoryRootNewIndex >= 0
}

// newRowIDs finds the ids the multi-row INSERT gave the rows linkSameBatchRows needs: each linked row and each row it links to. A
// child forked by a process the same batch created is the common case, so resolving these links one row at a time, as the rarer
// re-exec links are, would undo the batching. Each new row is found again by its (host_id, pid, source_event_id), which is unique
// because every write path stamps the event that created the row and a replayed event creates nothing. The result is indexed like
// rows, 0 where no id was needed.
func newRowIDs(ctx context.Context, ext sqlx.ExtContext, rows []NewProcessRow) ([]int64, error) {
	need := map[int]struct{}{}
	for i, r := range rows {
		if !sameBatchLinked(r) {
			continue
		}
		need[i] = struct{}{}
		for _, j := range []int{r.ParentNewIndex, r.StoryRootNewIndex} {
			if j >= 0 {
				need[j] = struct{}{}
			}
		}
	}
	wanted := slices.Sorted(maps.Keys(need))
	ids := make([]int64, len(rows))
	for start := 0; start < len(wanted); start += loadKeyChunk {
		if err := findNewRowIDs(ctx, ext, rows, wanted[start:min(start+loadKeyChunk, len(wanted))], ids); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// newRowKey is the identity newRowIDs finds a just-inserted row by.
type newRowKey struct {
	hostID        string
	pid           int
	sourceEventID string
}

func rowKeyOf(p api.Process) newRowKey {
	return newRowKey{hostID: p.HostID, pid: p.PID, sourceEventID: *p.SourceEventID}
}

func findNewRowIDs(ctx context.Context, ext sqlx.ExtContext, rows []NewProcessRow, wanted []int, ids []int64) error {
	var sb strings.Builder
	args := make([]any, 0, len(wanted)*3)
	index := make(map[newRowKey]int, len(wanted))
	sb.WriteString("SELECT id, host_id, pid, source_event_id FROM processes WHERE (host_id, pid, source_event_id) IN (")
	for n, i := range wanted {
		if n > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("(?, ?, ?)")
		key := rowKeyOf(rows[i].Proc)
		index[key] = i
		args = append(args, key.hostID, key.pid, key.sourceEventID)
	}
	sb.WriteString(")")
	var found []struct {
		ID            int64  `db:"id"`
		HostID        string `db:"host_id"`
		PID           int    `db:"pid"`
		SourceEventID string `db:"source_event_id"`
	}
	if err := sqlx.SelectContext(ctx, ext, &found, sb.String(), args...); err != nil {
		return fmt.Errorf("find %d same-batch rows: %w", len(wanted), err)
	}
	for _, f := range found {
		ids[index[newRowKey{hostID: f.HostID, pid: f.PID, sourceEventID: f.SourceEventID}]] = f.ID
	}
	return nil
}

// linkSameBatchRows sets parent_process_id and story_root_id on new rows that link to rows created in the same batch, once ids
// holds every inserted row's id. A dropped row (id 0) is neither linked nor linked to, which leaves the link NULL.
func linkSameBatchRows(ctx context.Context, ext sqlx.ExtContext, rows []NewProcessRow, ids []int64) error {
	var linked []int
	for i, r := range rows {
		if sameBatchLinked(r) && ids[i] != 0 {
			linked = append(linked, i)
		}
	}
	for start := 0; start < len(linked); start += processUpdateChunk {
		if err := linkRowsChunk(ctx, ext, rows, ids, linked[start:min(start+processUpdateChunk, len(linked))]); err != nil {
			return err
		}
	}
	return nil
}

// linkRowsChunk writes one set-based UPDATE for up to processUpdateChunk linked rows. A column only gets a CASE when some row in the
// chunk links by it, and a row whose targets were all dropped writes nothing.
func linkRowsChunk(ctx context.Context, ext sqlx.ExtContext, rows []NewProcessRow, ids []int64, linked []int) error {
	var sets []string
	var args []any
	for _, col := range []struct {
		name   string
		target func(NewProcessRow) int
	}{
		{"parent_process_id", func(r NewProcessRow) int { return r.ParentNewIndex }},
		{"story_root_id", func(r NewProcessRow) int { return r.StoryRootNewIndex }},
	} {
		var arms strings.Builder
		for _, i := range linked {
			if j := col.target(rows[i]); j >= 0 && ids[j] != 0 {
				arms.WriteString(" WHEN ? THEN ?")
				args = append(args, ids[i], ids[j])
			}
		}
		if arms.Len() > 0 {
			sets = append(sets, col.name+" = CASE id"+arms.String()+" ELSE "+col.name+" END")
		}
	}
	if len(sets) == 0 {
		return nil
	}
	query := "UPDATE processes SET " + strings.Join(sets, ", ") + " WHERE id IN (?" + strings.Repeat(", ?", len(linked)-1) + ")"
	for _, i := range linked {
		args = append(args, ids[i])
	}
	if _, err := ext.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("link %d same-batch rows: %w", len(linked), err)
	}
	return nil
}

// insertNewRowsChunk emits one multi-row INSERT for up to insertRowChunk rows. Used only on the no-same-batch-link path, so every
// previous_exec_id is already a real id (or nil) and no post-insert id resolution is needed.
func insertNewRowsChunk(ctx context.Context, ext sqlx.ExtContext, rows []NewProcessRow) error {
	var sb strings.Builder
	sb.WriteString(insertProcessColumns)
	sb.WriteString(" VALUES ")
	args := make([]any, 0, len(rows)*23)
	for i, r := range rows {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(insertProcessPlaceholders)
		args = appendInsertProcessArgs(args, r.Proc)
	}
	if _, err := ext.ExecContext(ctx, sb.String(), args...); err != nil {
		return fmt.Errorf("batch insert %d process rows: %w", len(rows), err)
	}
	return nil
}

// hasSameBatchLink reports whether any new row links to a predecessor created in the same batch (PrevNewIndex >= 0), which forces
// the per-row insert path so the link can be resolved to a real id.
func hasSameBatchLink(rows []NewProcessRow) bool {
	for _, r := range rows {
		if r.PrevNewIndex >= 0 {
			return true
		}
	}
	return false
}

// insertNewRowsLinked inserts the new rows one at a time in creation order, resolving each same-batch re-exec link to the real id
// the predecessor received. Creation order guarantees a predecessor is inserted before the child that links to it. When dropPoison
// is true a per-row permanent data error drops that row (and any later row whose predecessor was dropped loses its link) instead of
// failing; a transient error always aborts. A dropped row is logged so an unpersistable (e.g. attacker-influenced) process row
// leaves an audit trail rather than vanishing silently (issue #535 review).
func insertNewRowsLinked(ctx context.Context, ext sqlx.ExtContext, rows []NewProcessRow, dropPoison bool, logger *slog.Logger) error {
	ids := make([]int64, len(rows)) // assigned id per row; 0 for a row that was dropped or has no predecessor
	for i, r := range rows {
		if r.PrevNewIndex >= 0 {
			// Link to a predecessor created earlier in this batch. A dropped predecessor (id 0) leaves the link nil.
			if prevID := ids[r.PrevNewIndex]; prevID != 0 {
				r.Proc.PreviousExecID = &prevID
			} else if dropPoison {
				r.Proc.PreviousExecID = nil
			}
		}
		id, err := insertOneProcessRow(ctx, ext, r.Proc)
		if err != nil {
			if dropPoison && IsPermanentDataError(err) {
				logger.WarnContext(ctx, "process row dropped: permanent data error at flush",
					"host_id", r.Proc.HostID, "pid", r.Proc.PID, "err", err)
				continue // drop the poison row; ids[i] stays 0
			}
			return err
		}
		ids[i] = id
	}
	// The creator and story-root links are written once every row has its id, as on the batched path: a row can be its own story
	// root, so its id is not known until after its own INSERT.
	return linkSameBatchRows(ctx, ext, rows, ids)
}

// insertOneProcessRow inserts a single process row on the given executor and returns its auto-increment id. Shares the column list
// and arg order with the batched INSERT and the per-event InsertProcess.
func insertOneProcessRow(ctx context.Context, ext sqlx.ExtContext, p api.Process) (int64, error) {
	res, err := ext.ExecContext(ctx, insertProcessColumns+" VALUES "+insertProcessPlaceholders,
		appendInsertProcessArgs(nil, p)...)
	if err != nil {
		return 0, fmt.Errorf("insert process row (host=%s pid=%d): %w", p.HostID, p.PID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("insert process row last id: %w", err)
	}
	return id, nil
}

// updateRowsBatched applies the modified-row updates with chunked, set-based CASE statements (one statement per chunk of rows
// rather than one per row). Each row's full mutable column set is rewritten to its final value.
func (s *Store) updateRowsBatched(ctx context.Context, ext sqlx.ExtContext, updates []ProcessRowUpdate) error {
	for start := 0; start < len(updates); start += processUpdateChunk {
		end := min(start+processUpdateChunk, len(updates))
		if err := updateRowsChunk(ctx, ext, updates[start:end]); err != nil {
			return err
		}
	}
	return nil
}

// mutableUpdateColumns is the set CASE-keyed by id in the batched UPDATE, in a fixed order shared by the SQL builder and the arg
// appender. Immutable identity/fork columns are intentionally excluded.
var mutableUpdateColumns = []string{
	"path", "args", "uid", "gid", "code_signing", "sha256", "cdhash",
	"exec_time_ns", "pidversion", "exec_event_id", "exit_time_ns", "exit_ingested_at_ns", "exit_reason", "exit_code",
	"exit_event_id", "last_seen_ns",
	"original_ppid", "responsible_pid", "responsible_pidversion", "session_id", "group_id", "tty", "start_time_ns", "cwd",
	"launchd_jobs", "reparented", "agent_session_id", "agent_name", "agent_version", "tool_call_group",
}

func updateRowsChunk(ctx context.Context, ext sqlx.ExtContext, updates []ProcessRowUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString("UPDATE processes SET ")
	// Pre-size: each column contributes one CASE arm (id + value) per row; the trailing IN list one id per row.
	args := make([]any, 0, len(updates)*(len(mutableUpdateColumns)*2+1))
	for c, col := range mutableUpdateColumns {
		if c > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(col)
		sb.WriteString(" = CASE id")
		for _, u := range updates {
			sb.WriteString(" WHEN ? THEN ?")
			args = append(args, u.ID, updateColumnValue(col, u))
		}
		sb.WriteString(" END")
	}
	sb.WriteString(" WHERE id IN (")
	for i, u := range updates {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("?")
		args = append(args, u.ID)
	}
	sb.WriteString(")")
	if _, err := ext.ExecContext(ctx, sb.String(), args...); err != nil {
		return fmt.Errorf("batch update %d process rows: %w", len(updates), err)
	}
	return nil
}

// updateColumnValue returns the bound value for a column in the CASE-keyed UPDATE. Centralised so the column order in the SQL
// builder and the values stay in lockstep.
func updateColumnValue(col string, u ProcessRowUpdate) any {
	switch col {
	case "path":
		return u.Path
	case "args":
		return u.Args
	case "uid":
		return u.UID
	case "gid":
		return u.GID
	case "code_signing":
		return u.CodeSigning
	case "sha256":
		return u.SHA256
	case "cdhash":
		return u.CDHash
	case "exec_time_ns":
		return u.ExecTimeNs
	case "pidversion":
		return u.PIDVersion
	case "exec_event_id":
		return u.ExecEventID
	case "exit_time_ns":
		return u.ExitTimeNs
	case "exit_ingested_at_ns":
		return u.ExitIngestedAtNs
	case "exit_reason":
		return u.ExitReason
	case "exit_code":
		return u.ExitCode
	case "exit_event_id":
		return u.ExitEventID
	case "last_seen_ns":
		return u.LastSeenNs
	case "original_ppid":
		return u.Lineage.OriginalPPID
	case "responsible_pid":
		return u.Lineage.ResponsiblePID
	case "responsible_pidversion":
		return u.Lineage.ResponsiblePIDVersion
	case "session_id":
		return u.Lineage.SessionID
	case "group_id":
		return u.Lineage.GroupID
	case "tty":
		return u.Lineage.TTY
	case "start_time_ns":
		return u.Lineage.StartTimeNs
	case "cwd":
		return u.Lineage.CWD
	case "launchd_jobs":
		return u.Lineage.LaunchdJobs
	case "reparented":
		return u.Lineage.Reparented
	case "agent_session_id":
		return u.Agent.AgentSessionID
	case "agent_name":
		return u.Agent.AgentName
	case "agent_version":
		return u.Agent.AgentVersion
	case "tool_call_group":
		return u.ToolCallGroup
	default:
		panic("mysql: unknown mutable update column " + col) // unreachable: driven by mutableUpdateColumns
	}
}

// flushProcessBatchPerRow re-applies the plan one statement at a time in a fresh transaction, dropping (and logging) rows that hit a
// permanent data error so one unpersistable row cannot wedge the batch (issue #379) while still leaving an audit trail (issue #535
// review). Used only after the batched fast path failed with a permanent error, so this slow path runs at most once per poison batch.
func (s *Store) flushProcessBatchPerRow(ctx context.Context, plan ProcessBatchPlan) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin process-batch per-row tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if err := insertNewRowsLinked(ctx, tx, plan.NewRows, true, s.logger); err != nil {
		return err
	}
	for _, u := range plan.Updates {
		if err := updateRowsChunk(ctx, tx, []ProcessRowUpdate{u}); err != nil {
			if IsPermanentDataError(err) {
				s.logger.WarnContext(ctx, "process row update dropped: permanent data error at flush", "id", u.ID, "err", err)
				continue // drop the poison update; the rest still applies
			}
			return err
		}
	}
	// A backfill writes only a signature already parsed from an event payload, so it is never the poison value this path isolates;
	// it is applied as the fast path applies it.
	if err := backfillSigning(ctx, tx, plan.SigningBackfills); err != nil {
		return err
	}
	// A sighting shares its host_id with the event that produced it, so a host_id too long for the column poisons both; it is
	// isolated the same way as a row.
	for _, sighting := range plan.Sessions {
		if err := upsertSessions(ctx, tx, []SessionSighting{sighting}); err != nil {
			if IsPermanentDataError(err) {
				s.logger.WarnContext(ctx, "agent session sighting dropped: permanent data error at flush", "host_id", sighting.HostID,
					"err", err)
				continue
			}
			return err
		}
	}
	// A bypass sighting's session id comes from a transcript, so one too long for the column poisons only that sighting.
	for _, sighting := range plan.Bypass {
		if err := upsertBypass(ctx, tx, []BypassSighting{sighting}); err != nil {
			if IsPermanentDataError(err) {
				s.logger.WarnContext(ctx, "agent session bypass sighting dropped: permanent data error at flush", "host_id", sighting.HostID,
					"err", err)
				continue
			}
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit process-batch per-row tx: %w", err)
	}
	return nil
}
