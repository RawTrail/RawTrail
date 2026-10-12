//go:build integration

// Creator links and generation-bound exits (issue #1280). A fork names its creator by (parent_pid, parent_pidversion), and the graph
// builder resolves that to the creator's row, falling back by time only when the generation it names is no longer on a row. An exit
// that carries its generation closes that generation's row and never a newer one. Generations follow the kernel's rule: one counter,
// bumped by every fork and every exec.

package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fleetdm/edr/server/detection/api"
)

// forkAt returns the single row for pid whose fork_time_ns is forkTs and that never exec'd.
func forkAt(t *testing.T, rows []normRow, pid int, forkTs int64) normRow {
	t.Helper()
	var found []normRow
	for _, r := range rows {
		if r.PID == pid && r.ForkTimeNs == forkTs && r.ExecTimeNs == nil {
			found = append(found, r)
		}
	}
	require.Len(t, found, 1, "expected exactly one fork-only row for pid %d forked at %d", pid, forkTs)
	return found[0]
}

// parentOf returns the row the child links to, failing the test when the link is unset.
func parentOf(t *testing.T, rows []normRow, child normRow) normRow {
	t.Helper()
	require.GreaterOrEqual(t, child.ParentRank, 0, "pid %d must link to its creator", child.PID)
	return rows[child.ParentRank]
}

// processInBatches applies each batch in turn on one builder and returns the host's rows.
func processInBatches(t *testing.T, batches ...[]api.Event) []normRow {
	t.Helper()
	b, _, db := twoBuilders(t)
	host := "parent-" + t.Name()
	for _, batch := range batches {
		require.NoError(t, b.ProcessBatch(t.Context(), rewriteHost(batch, host)))
	}
	return dumpNormalizedForest(t, db, t.Context(), host)
}

// earlierParent is pid 500's first process: forked at 1000 as generation 7300, exec'd at 1100 as 7301, exited at 1500. A later process
// reuses pid 500 from 1600 (7350) and execs at 1700 (7351).
func earlierParent() []api.Event {
	return []api.Event{
		lineageEvt("f-a", "fork", 1000, `{"child_pid":500,"parent_pid":1,"pidversion":7300,"parent_pidversion":1}`),
		lineageEvt("e-a", "exec", 1100, `{"pid":500,"ppid":1,"path":"/bin/zsh","pidversion":7301,"parent_pidversion":1}`),
		lineageEvt("x-a", "exit", 1500, `{"pid":500,"exit_code":0,"pidversion":7301}`),
	}
}

func laterParent() []api.Event {
	return []api.Event{
		lineageEvt("f-b", "fork", 1600, `{"child_pid":500,"parent_pid":1,"pidversion":7350,"parent_pidversion":1}`),
		lineageEvt("e-b", "exec", 1700, `{"pid":500,"ppid":1,"path":"/usr/bin/python3","pidversion":7351,"parent_pidversion":1}`),
	}
}

// spec:server-process-graph-builder/process-creator-resolution/the-exact-generation-wins-over-a-reused-pid
// spec:server-process-graph-builder/process-creator-resolution/a-newer-generation-is-not-a-creator
// spec:server-process-graph-builder/process-creator-resolution/a-child-forked-before-its-parent-s-first-exec-links-to-that-row
func TestParentLink_ResolvesTheCreatorGeneration(t *testing.T) {
	t.Parallel()

	t.Run("the exact generation wins over a reused pid", func(t *testing.T) {
		t.Parallel()
		// The child's fork is stamped after the later process forked, so the time rule alone would pick the later process.
		rows := processInBatches(t, append(earlierParent(), laterParent()...), []api.Event{
			lineageEvt("f-child", "fork", 1650, `{"child_pid":600,"parent_pid":500,"pidversion":7360,"parent_pidversion":7301}`),
		})
		parent := parentOf(t, rows, forkAt(t, rows, 600, 1650))
		assert.Equal(t, "/bin/zsh", parent.Path)
		assert.Equal(t, new(uint32(7301)), parent.PIDVersion)
	})

	t.Run("a newer generation is not a creator", func(t *testing.T) {
		t.Parallel()
		// The earlier process left no row (its events were lost), so the only row for pid 500 is the later one.
		rows := processInBatches(t, laterParent(), []api.Event{
			lineageEvt("f-child", "fork", 1800, `{"child_pid":600,"parent_pid":500,"pidversion":7360,"parent_pidversion":7301}`),
		})
		assert.Equal(t, -1, forkAt(t, rows, 600, 1800).ParentRank)
	})

	t.Run("a child forked before its parent's first exec links to that row", func(t *testing.T) {
		t.Parallel()
		// The exec rewrote the row's generation from 7300 to 7301 before the child's fork was processed.
		rows := processInBatches(t, earlierParent()[:2], []api.Event{
			lineageEvt("f-child", "fork", 1050, `{"child_pid":600,"parent_pid":500,"pidversion":7302,"parent_pidversion":7300}`),
		})
		assert.Equal(t, "/bin/zsh", parentOf(t, rows, forkAt(t, rows, 600, 1050)).Path)
	})

	t.Run("a dropped exec leaves the parent one generation behind", func(t *testing.T) {
		t.Parallel()
		// The parent re-exec'd as 7320 but that exec was lost, so its row still holds 7301.
		rows := processInBatches(t, earlierParent()[:2], []api.Event{
			lineageEvt("f-child", "fork", 1200, `{"child_pid":600,"parent_pid":500,"pidversion":7330,"parent_pidversion":7320}`),
		})
		assert.Equal(t, "/bin/zsh", parentOf(t, rows, forkAt(t, rows, 600, 1200)).Path)
	})

	t.Run("a generation reused after a reboot is bounded by time", func(t *testing.T) {
		t.Parallel()
		// pidversion restarts at each boot, so a later boot's process can hold the same pid and generation. It forked after the child,
		// so it cannot be the child's creator.
		rows := processInBatches(t, append(earlierParent()[:2],
			lineageEvt("f-reboot", "fork", 5000, `{"child_pid":500,"parent_pid":1,"pidversion":7300,"parent_pidversion":1}`),
			lineageEvt("e-reboot", "exec", 5100, `{"pid":500,"ppid":1,"path":"/usr/sbin/cron","pidversion":7301,"parent_pidversion":1}`),
		), []api.Event{
			lineageEvt("f-child", "fork", 1200, `{"child_pid":600,"parent_pid":500,"pidversion":7330,"parent_pidversion":7301}`),
		})
		assert.Equal(t, "/bin/zsh", parentOf(t, rows, forkAt(t, rows, 600, 1200)).Path)
	})

	t.Run("a parent recorded without a generation is matched by time", func(t *testing.T) {
		t.Parallel()
		rows := processInBatches(t, []api.Event{
			lineageEvt("f-legacy", "fork", 1000, `{"child_pid":500,"parent_pid":1}`),
			lineageEvt("e-legacy", "exec", 1100, `{"pid":500,"ppid":1,"path":"/bin/bash"}`),
		}, []api.Event{
			lineageEvt("f-child", "fork", 1200, `{"child_pid":600,"parent_pid":500,"pidversion":7330,"parent_pidversion":7320}`),
		})
		assert.Equal(t, "/bin/bash", parentOf(t, rows, forkAt(t, rows, 600, 1200)).Path)
	})
}

// spec:server-process-graph-builder/process-creator-resolution/a-daemon-links-to-its-creator-after-a-double-fork
func TestParentLink_DoubleFork(t *testing.T) {
	t.Parallel()
	events := append(lineageScenario(),
		lineageEvt("e-daemon-reexec", "exec", 2400, `{"pid":700,"ppid":1,"path":"/usr/bin/caffeinate","pidversion":7418,`+
			`"original_ppid":650,"parent_pidversion":7}`))

	t.Run("one batch links through a creator created in the same batch", func(t *testing.T) {
		t.Parallel()
		rows := processInBatches(t, events)
		daemon := generationAt(t, rows, 700, 2300)
		subshell := parentOf(t, rows, daemon)
		assert.Equal(t, 650, subshell.PID)
		assert.Equal(t, new(uint32(7410)), subshell.PIDVersion)
		shell := parentOf(t, rows, subshell)
		assert.Equal(t, 600, shell.PID)
		assert.Equal(t, "/bin/zsh", shell.Path, "the subshell forked from the shell's first image, not its later curl")
		assert.Equal(t, daemon.ParentRank, generationAt(t, rows, 700, 2400).ParentRank, "a re-exec keeps the creator link")
	})

	t.Run("one batch with no re-exec links after the batched insert", func(t *testing.T) {
		t.Parallel()
		// Without a re-exec in the batch the flush inserts every row in one statement, so the same-batch links are set afterwards.
		rows := processInBatches(t, lineageScenario()[:6])
		subshell := parentOf(t, rows, generationAt(t, rows, 700, 2300))
		assert.Equal(t, 650, subshell.PID)
		assert.Equal(t, 600, parentOf(t, rows, subshell).PID)
	})

	t.Run("one batch matches one event per batch", func(t *testing.T) {
		t.Parallel()
		bRef, bBatch, db := twoBuilders(t)
		requireBatchInvariant(t, db, bRef, bBatch, events, "double-fork-"+t.Name())
	})
}

// spec:server-process-graph-builder/process-creator-resolution/events-without-a-generation-leave-the-link-unset
func TestParentLink_EventsWithoutAGeneration(t *testing.T) {
	t.Parallel()
	// launchd is on record, so a reparented process that named it would link to it.
	launchd := lineageEvt("s-launchd", "exec", 10, `{"pid":1,"ppid":0,"path":"/sbin/launchd","snapshot":true,"pidversion":7}`)
	rows := processInBatches(t, append([]api.Event{launchd}, lineageScenario()...))

	assert.Equal(t, -1, generationAt(t, rows, 900, 4100).ParentRank, "a fork without parent_pidversion")
	assert.Equal(t, -1, generationAt(t, rows, 800, 3100).ParentRank, "a reparented process first seen at exec")

	t.Run("a process first seen at exec links to the parent it names", func(t *testing.T) {
		t.Parallel()
		// In a later batch, so the parent's rows come from the preload rather than from this batch.
		rows := processInBatches(t, lineageScenario(), []api.Event{
			lineageEvt("e-orphan-child", "exec", 3200, `{"pid":810,"ppid":600,"path":"/bin/ls","pidversion":7440,`+
				`"original_ppid":600,"parent_pidversion":7420}`)})
		assert.Equal(t, "/usr/bin/curl", parentOf(t, rows, generationAt(t, rows, 810, 3200)).Path)
	})
}

// A replayed batch must not change a link: the creator rows already exist, and the replayed events are already applied.
func TestParentLink_ReplayIsANoOp(t *testing.T) {
	t.Parallel()
	b, _, db := twoBuilders(t)
	host := "parent-replay-" + t.Name()
	events := rewriteHost(lineageScenario(), host)
	require.NoError(t, b.ProcessBatch(t.Context(), events))
	first := dumpNormalizedForest(t, db, t.Context(), host)
	require.NoError(t, b.ProcessBatch(t.Context(), events))
	assert.Equal(t, first, dumpNormalizedForest(t, db, t.Context(), host))
}

// spec:server-process-graph-builder/exit-binding-by-process-generation/an-exit-never-closes-a-newer-generation
func TestExitBinding_ByGeneration(t *testing.T) {
	t.Parallel()

	t.Run("a late exit of an earlier process leaves the newer one open", func(t *testing.T) {
		t.Parallel()
		rows := processInBatches(t, laterParent(), []api.Event{
			lineageEvt("x-late", "exit", 1800, `{"pid":500,"exit_code":0,"pidversion":7301}`),
		})
		assert.Nil(t, generationAt(t, rows, 500, 1700).ExitTimeNs)
	})

	t.Run("with two open rows the exit closes its own generation", func(t *testing.T) {
		t.Parallel()
		// The later process was materialized first, so the earlier process's fork did not close it as a reused pid, and both rows
		// are open when the earlier process's exit arrives, stamped after the later fork.
		rows := processInBatches(t, laterParent(), earlierParent()[:1], []api.Event{
			lineageEvt("x-a", "exit", 1750, `{"pid":500,"exit_code":3,"pidversion":7300}`),
		})
		earlier, later := forkAt(t, rows, 500, 1000), generationAt(t, rows, 500, 1700)
		require.NotNil(t, earlier.ExitCode)
		assert.Equal(t, 3, *earlier.ExitCode)
		assert.Nil(t, later.ExitTimeNs)
	})

	t.Run("an exit after a dropped exec closes the older row", func(t *testing.T) {
		t.Parallel()
		rows := processInBatches(t, append(earlierParent()[:2],
			lineageEvt("x-dropped", "exit", 1400, `{"pid":500,"exit_code":0,"pidversion":7320}`)))
		assert.NotNil(t, generationAt(t, rows, 500, 1100).ExitTimeNs)
	})

	t.Run("an exit closes a row recorded without a generation", func(t *testing.T) {
		t.Parallel()
		rows := processInBatches(t, []api.Event{
			lineageEvt("f-legacy", "fork", 1000, `{"child_pid":500,"parent_pid":1}`),
			lineageEvt("e-legacy", "exec", 1100, `{"pid":500,"ppid":1,"path":"/bin/bash"}`),
			lineageEvt("x-legacy", "exit", 1200, `{"pid":500,"exit_code":0,"pidversion":7301}`),
		})
		assert.NotNil(t, generationAt(t, rows, 500, 1100).ExitTimeNs)
	})
}

// spec:server-process-graph-builder/exit-binding-by-process-generation/a-buffered-exit-skips-a-newer-snapshot
func TestExitBinding_BufferedExitSkipsANewerSnapshot(t *testing.T) {
	t.Parallel()
	snapshot := func(pidversion string) []api.Event {
		return []api.Event{lineageEvt("s-snap", "exec", 2000, `{"pid":500,"ppid":1,"path":"/bin/late","snapshot":true,`+
			`"pidversion":`+pidversion+`}`)}
	}
	exit := []api.Event{lineageEvt("x-early", "exit", 1900, `{"pid":500,"exit_code":0,"pidversion":7400}`)}

	newer := processInBatches(t, exit, snapshot("7450"))
	assert.Nil(t, generationAt(t, newer, 500, 2000).ExitTimeNs, "a snapshot of a newer generation stays open")

	same := processInBatches(t, exit, snapshot("7400"))
	exited := generationAt(t, same, 500, 1900)
	assert.NotNil(t, exited.ExitTimeNs, "the snapshot of the exiting generation is born exited")
}
