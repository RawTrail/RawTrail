//go:build integration

// Lineage identity on process rows (issue #1280). The extension reports, on fork, the creator's generation and, on exec, the
// creator's pid, the responsible process, the session and process group, the terminal, the start time and the working directory
// (issue #1277). The graph builder stores them on the row for each of its write paths: a fork, a first exec after fork, an exec
// with no fork seen, and a same-pid re-exec. Scenarios mirror what edr-dev produced for a double-forked daemon.

package tests

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

func lineageEvt(id, eventType string, ts int64, payload string) api.Event {
	return api.Event{EventID: id, HostID: "x", TimestampNs: ts, IngestedAtNs: ts + 1, EventType: eventType,
		Payload: json.RawMessage(payload)}
}

// lineageScenario is a shell (pid 600) that a terminal session started, which runs a command and then double-forks a daemon (pid
// 700) through an intermediate subshell (pid 650) that exits before the daemon execs. A process first seen at exec (pid 800) and an
// agent without the lineage fields (pid 900) round it out.
func lineageScenario() []api.Event {
	return []api.Event{
		lineageEvt("f-shell", "fork", 1000, `{"child_pid":600,"parent_pid":500,"pidversion":7400,"parent_pidversion":7300}`),
		lineageEvt("e-shell", "exec", 1100, `{"pid":600,"ppid":500,"path":"/bin/zsh","args":["zsh"],"cwd":"/Users/dev/project",`+
			`"uid":501,"gid":20,"pidversion":7401,"original_ppid":500,"parent_pidversion":7300,"responsible_pid":410,`+
			`"responsible_pidversion":5391,"session_id":600,"group_id":600,"tty":"/dev/ttys004","start_time_ns":1000}`),
		lineageEvt("f-sub", "fork", 2000, `{"child_pid":650,"parent_pid":600,"pidversion":7410,"parent_pidversion":7401}`),
		lineageEvt("f-daemon", "fork", 2100, `{"child_pid":700,"parent_pid":650,"pidversion":7416,"parent_pidversion":7410}`),
		lineageEvt("x-sub", "exit", 2200, `{"pid":650,"exit_code":0}`),
		lineageEvt("e-daemon", "exec", 2300, `{"pid":700,"ppid":1,"path":"/bin/sleep","args":["sleep","81"],"cwd":"/private/tmp",`+
			`"uid":501,"gid":20,"pidversion":7417,"original_ppid":650,"parent_pidversion":7,"responsible_pid":410,`+
			`"responsible_pidversion":5391,"session_id":600,"group_id":650,"start_time_ns":2100}`),
		lineageEvt("e-reexec", "exec", 3000, `{"pid":600,"ppid":500,"path":"/usr/bin/curl","args":["curl"],"cwd":"/Users/dev",`+
			`"uid":501,"gid":20,"pidversion":7420,"original_ppid":500,"parent_pidversion":7300,"responsible_pid":410,`+
			`"responsible_pidversion":5391,"session_id":600,"group_id":600,"tty":"/dev/ttys004","start_time_ns":1000}`),
		lineageEvt("e-orphan", "exec", 3100, `{"pid":800,"ppid":1,"path":"/usr/libexec/helper","args":[],"cwd":"/",`+
			`"uid":0,"gid":0,"pidversion":7430,"original_ppid":640,"parent_pidversion":7,"session_id":800,"group_id":800}`),
		lineageEvt("f-legacy", "fork", 4000, `{"child_pid":900,"parent_pid":600}`),
		lineageEvt("e-legacy", "exec", 4100, `{"pid":900,"ppid":600,"path":"/bin/ls","args":["ls"],"cwd":"","uid":501,"gid":20}`),
	}
}

// spec:server-process-graph-builder/process-lineage-persistence/an-exec-stores-its-lineage-on-the-row
// spec:server-process-graph-builder/process-lineage-persistence/a-daemonized-process-is-marked-reparented
// spec:server-process-graph-builder/process-lineage-persistence/a-legacy-event-leaves-the-lineage-unset
func TestLineageRows_EachWritePathStoresTheLineage(t *testing.T) {
	t.Parallel()
	b, _, db := twoBuilders(t)
	ctx := t.Context()
	host := "lineage-" + t.Name()
	require.NoError(t, b.ProcessBatch(ctx, rewriteHost(lineageScenario(), host)))
	rows := dumpNormalizedForest(t, db, ctx, host)

	t.Run("first exec after fork writes the lineage onto the fork's row", func(t *testing.T) {
		shell := generationAt(t, rows, 600, 1100)
		assert.Equal(t, 500, shell.PPID)
		assert.Equal(t, new(uint32(7300)), shell.ParentPIDVersion, "the fork's creator generation")
		assert.Equal(t, api.ExecLineage{
			OriginalPPID: new(500), ResponsiblePID: new(410), ResponsiblePIDVersion: new(uint32(5391)), SessionID: new(600),
			GroupID: new(600), TTY: new("/dev/ttys004"), StartTimeNs: new(int64(1000)), CWD: new("/Users/dev/project"),
		}, shell.ExecLineage)
	})

	t.Run("a daemon keeps its creator and is marked reparented", func(t *testing.T) {
		daemon := generationAt(t, rows, 700, 2300)
		assert.Equal(t, 650, daemon.PPID, "the exec's ppid of 1 names launchd, not the creator the fork recorded")
		assert.Equal(t, new(uint32(7410)), daemon.ParentPIDVersion, "the creator's generation from the fork")
		assert.Equal(t, new(650), daemon.OriginalPPID)
		assert.True(t, daemon.Reparented)
		assert.Equal(t, new(650), daemon.GroupID, "the process group survives the double fork")
		assert.Nil(t, daemon.TTY, "a daemon has no controlling terminal")
	})

	t.Run("a re-exec keeps the parent and takes the new image's lineage", func(t *testing.T) {
		curl := generationAt(t, rows, 600, 3000)
		assert.Equal(t, 500, curl.PPID)
		assert.Equal(t, new(uint32(7300)), curl.ParentPIDVersion)
		assert.Equal(t, new("/Users/dev"), curl.CWD)
		assert.False(t, curl.Reparented)
	})

	t.Run("an exec with no fork seen names the parent it reports", func(t *testing.T) {
		orphan := generationAt(t, rows, 800, 3100)
		assert.Equal(t, 1, orphan.PPID)
		assert.Equal(t, new(uint32(7)), orphan.ParentPIDVersion, "ppid and its generation stay a pair")
		assert.Equal(t, new(640), orphan.OriginalPPID)
		assert.True(t, orphan.Reparented)
		assert.Nil(t, orphan.TTY)
	})

	t.Run("an agent without the lineage fields leaves them unset", func(t *testing.T) {
		legacy := generationAt(t, rows, 900, 4100)
		assert.Equal(t, 600, legacy.PPID)
		assert.Nil(t, legacy.ParentPIDVersion)
		assert.Equal(t, api.ExecLineage{}, legacy.ExecLineage, "an empty cwd is stored as unknown")
	})
}

// The lineage must come out the same whether the batch fold or one-event batches wrote it, which is what keeps the overlay and the
// store in step for the new columns.
func TestLineageRows_BatchMatchesPerEvent(t *testing.T) {
	t.Parallel()
	bRef, bBatch, db := twoBuilders(t)
	requireBatchInvariant(t, db, bRef, bBatch, lineageScenario(), "lineage-"+t.Name())
}

// A replayed batch must not change any stored lineage (migration 00011's idempotence).
func TestLineageRows_ReplayIsANoOp(t *testing.T) {
	t.Parallel()
	b, _, db := twoBuilders(t)
	ctx := t.Context()
	host := "lineage-replay-" + t.Name()
	events := rewriteHost(lineageScenario(), host)
	require.NoError(t, b.ProcessBatch(ctx, events))
	first := dumpNormalizedForest(t, db, ctx, host)
	require.NoError(t, b.ProcessBatch(ctx, events))
	assert.Equal(t, first, dumpNormalizedForest(t, db, ctx, host))
}
