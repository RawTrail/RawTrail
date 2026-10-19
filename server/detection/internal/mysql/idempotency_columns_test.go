package mysql_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/internal/mysql"
)

// The graph builder drives writes through the batch overlay + set-based flush, so the per-event Store write methods below are reached
// directly only here. These tests pin the migration-00011 idempotency columns (source_event_id / exec_event_id / exit_event_id), the
// EventAlreadyApplied probe the builder guards on, the exit fork-time bound, and the monotonic last-seen bump.

// TestStore_EventAlreadyApplied pins the re-processing guard probe: a row records the fork/exec/exit event ids that materialized it,
// and EventAlreadyApplied reports true for any of them, scoped to (host_id, pid).
func TestStore_EventAlreadyApplied(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := t.Context()

	// source_event_id + exec_event_id are set at insert; exit_event_id is stamped only by the exit UPDATE, never at insert.
	_, err := s.InsertProcess(ctx, api.Process{
		HostID: "h", PID: 10, ForkTimeNs: 100,
		SourceEventID: new("fork-1"), ExecEventID: new("exec-1"),
	})
	require.NoError(t, err)
	affected, err := s.UpdateProcessExit(ctx, mysql.ProcessExitUpdate{
		HostID: "h", PID: 10, ExitTimeNs: 200, ExitIngestedAtNs: 201, ExitCode: 0, Reason: api.ExitReasonEvent, ExitEventID: "exit-1",
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), affected)

	for _, id := range []string{"fork-1", "exec-1", "exit-1"} {
		applied, err := s.EventAlreadyApplied(ctx, "h", 10, id)
		require.NoError(t, err)
		assert.True(t, applied, "event %s should read as already applied", id)
	}

	applied, err := s.EventAlreadyApplied(ctx, "h", 10, "never-seen")
	require.NoError(t, err)
	assert.False(t, applied, "an unknown event is not applied")

	applied, err = s.EventAlreadyApplied(ctx, "h", 999, "fork-1")
	require.NoError(t, err)
	assert.False(t, applied, "the probe is scoped to the pid")
}

// TestStore_UpdateProcessExit_ForkTimeBound pins that an exit only closes a row that forked at or before the exit, and stamps the
// closing event's id so a replay is recognized.
func TestStore_UpdateProcessExit_ForkTimeBound(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := t.Context()

	// A row that forked at 200; an exit at 100 (before its fork) must not close it.
	_, err := s.InsertProcess(ctx, api.Process{HostID: "h", PID: 20, ForkTimeNs: 200})
	require.NoError(t, err)

	affected, err := s.UpdateProcessExit(ctx, mysql.ProcessExitUpdate{
		HostID: "h", PID: 20, ExitTimeNs: 100, ExitIngestedAtNs: 101, ExitCode: 0, Reason: api.ExitReasonEvent, ExitEventID: "exit-stale",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(0), affected, "an exit before the row's fork must not close it")

	// An exit after the fork closes it and stamps exit_event_id.
	affected, err = s.UpdateProcessExit(ctx, mysql.ProcessExitUpdate{
		HostID: "h", PID: 20, ExitTimeNs: 300, ExitIngestedAtNs: 301, ExitCode: 7, Reason: api.ExitReasonEvent, ExitEventID: "exit-real",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), affected)

	got, err := s.GetProcessByPID(ctx, "h", 20, 300)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.ExitEventID)
	assert.Equal(t, "exit-real", *got.ExitEventID)

	applied, err := s.EventAlreadyApplied(ctx, "h", 20, "exit-real")
	require.NoError(t, err)
	assert.True(t, applied, "the closing exit is recognized on replay")
}

// TestStore_UpdateProcessExec_StampsExecEventID pins that the first-exec-after-fork UPDATE records the exec event's id.
func TestStore_UpdateProcessExec_StampsExecEventID(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := t.Context()

	_, err := s.InsertProcess(ctx, api.Process{HostID: "h", PID: 30, ForkTimeNs: 10})
	require.NoError(t, err)

	require.NoError(t, s.UpdateProcessExec(ctx, mysql.ProcessExecUpdate{
		HostID: "h", PID: 30, ExecTimeNs: 20, Path: "/bin/x", ExecEventID: new("exec-30"),
	}))

	got, err := s.GetProcessByPID(ctx, "h", 30, 20)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.ExecEventID)
	assert.Equal(t, "exec-30", *got.ExecEventID)

	applied, err := s.EventAlreadyApplied(ctx, "h", 30, "exec-30")
	require.NoError(t, err)
	assert.True(t, applied)
}

// TestStore_UpdateLastSeenForSnapshot_Monotonic pins that the freshness bump only advances: a later, then an earlier heartbeat leaves
// last_seen_ns at the maximum so a replayed earlier heartbeat cannot regress it.
func TestStore_UpdateLastSeenForSnapshot_Monotonic(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := t.Context()

	_, err := s.InsertProcess(ctx, api.Process{
		HostID: "h", PID: 40, ForkTimeNs: 10, ExecTimeNs: new(int64(10)), IsSnapshot: true, LastSeenNs: new(int64(10)),
	})
	require.NoError(t, err)

	require.NoError(t, s.UpdateLastSeenForSnapshot(ctx, "h", 40, 1000)) // advance
	require.NoError(t, s.UpdateLastSeenForSnapshot(ctx, "h", 40, 800))  // backward: must be ignored

	got, err := s.GetProcessByPID(ctx, "h", 40, 1000)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.LastSeenNs)
	assert.Equal(t, int64(1000), *got.LastSeenNs, "a backward heartbeat must not regress last_seen_ns")

	require.NoError(t, s.UpdateLastSeenForSnapshot(ctx, "h", 40, 1500)) // forward: advances
	got, err = s.GetProcessByPID(ctx, "h", 40, 1500)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.LastSeenNs)
	assert.Equal(t, int64(1500), *got.LastSeenNs)
}

// TestStore_ReExec_StampsEventIDs pins that a re-exec generation carries the exec event as both its source and exec event id, so a
// replayed re-exec is recognized rather than fabricating another generation.
func TestStore_ReExec_StampsEventIDs(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := t.Context()

	priorID, err := s.InsertProcess(ctx, api.Process{
		HostID: "h", PID: 50, ForkTimeNs: 10, ExecTimeNs: new(int64(11)), SourceEventID: new("fork-50"), ExecEventID: new("exec-50a"),
	})
	require.NoError(t, err)

	newID, reLinked, err := s.ReExec(ctx, priorID, 20, 21, api.Process{
		HostID: "h", PID: 50, ForkTimeNs: 10, ExecTimeNs: new(int64(20)),
		SourceEventID: new("exec-50b"), ExecEventID: new("exec-50b"),
	})
	require.NoError(t, err)
	assert.True(t, reLinked, "the live prior generation is chained")

	got, err := s.GetProcessByPID(ctx, "h", 50, 20)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, newID, got.ID)
	require.NotNil(t, got.SourceEventID)
	assert.Equal(t, "exec-50b", *got.SourceEventID)
	require.NotNil(t, got.PreviousExecID)
	assert.Equal(t, priorID, *got.PreviousExecID)

	applied, err := s.EventAlreadyApplied(ctx, "h", 50, "exec-50b")
	require.NoError(t, err)
	assert.True(t, applied)
}

// TestStore_UpdateProcessExit_ByGeneration pins the exit binding by kernel generation (issue #1280), which the batch overlay's
// exitTarget mirrors: the exact generation first, then the most recent older row, then an unversioned one, never a newer one. An exit
// without a generation keeps the pid-only rule.
func TestStore_UpdateProcessExit_ByGeneration(t *testing.T) {
	t.Parallel()
	type seed struct {
		name   string
		ver    *uint32
		forkNs int64
	}
	unversioned := seed{"unversioned", nil, 250}
	older := seed{"older", new(uint32(7300)), 200}
	exact := seed{"exact", new(uint32(7350)), 150}
	newer := seed{"newer", new(uint32(7400)), 300}
	cases := []struct {
		name    string
		rows    []seed
		exitVer *uint32
		closed  string // the seed the exit closes, or "" for none
	}{
		{"the exact generation beats an older row that forked later", []seed{older, exact, newer}, new(uint32(7350)), "exact"},
		{"an older row when the exact one is missing", []seed{older, newer}, new(uint32(7350)), "older"},
		{"an older row beats an unversioned one", []seed{unversioned, older}, new(uint32(7350)), "older"},
		{"an unversioned row when it is the only one", []seed{unversioned}, new(uint32(7350)), "unversioned"},
		{"never a newer generation", []seed{newer}, new(uint32(7350)), ""},
		{"an exit without a generation closes the newest fork", []seed{older, newer}, nil, "newer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)
			ctx := t.Context()
			ids := map[string]int64{}
			for _, r := range tc.rows {
				id, err := s.InsertProcess(ctx, api.Process{HostID: "h", PID: 30, ForkTimeNs: r.forkNs, PIDVersion: r.ver})
				require.NoError(t, err)
				ids[r.name] = id
			}
			affected, err := s.UpdateProcessExit(ctx, mysql.ProcessExitUpdate{
				HostID: "h", PID: 30, ExitTimeNs: 400, ExitIngestedAtNs: 401, ExitEventID: "exit-gen", PIDVersion: tc.exitVer,
			})
			require.NoError(t, err)
			assert.Equal(t, map[bool]int64{true: 1, false: 0}[tc.closed != ""], affected)
			for name, id := range ids {
				row, err := s.GetProcessByID(ctx, "h", id)
				require.NoError(t, err)
				assert.Equal(t, name == tc.closed, row.ExitTimeNs != nil, "row %s", name)
			}
		})
	}
}
