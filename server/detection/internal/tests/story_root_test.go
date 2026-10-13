//go:build integration

// Story roots and tool calls (issue #1281). The scenario follows the edr-dev capture of a real Claude Code 2.1.296 session started
// over SSH: launchd started sshd-session, which ran a login zsh, which ran claude; each Bash tool call is a zsh claude forked, leading
// its own process group, and carries CLAUDE_PID naming claude.

package tests

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/fleetdm/edr/server/detection/api"
	"github.com/fleetdm/edr/server/detection/internal/graph"
)

// toolCallEnv renders a tool call exec's markers: the session, the agent's self-description, and the agent's own pid.
func toolCallEnv(agentPID int) string {
	return `"env":{"CLAUDE_CODE_SESSION_ID":"` + sessionOne + `","AI_AGENT":"claude-code_2-1-296_agent","CLAUDE_PID":"` +
		strconv.Itoa(agentPID) + `"}`
}

func storyScenario() []api.Event {
	return []api.Event{
		lineageEvt("f-sshd", "fork", 900, `{"child_pid":80667,"parent_pid":1,"pidversion":1888040,"parent_pidversion":1}`),
		lineageEvt("e-sshd", "exec", 905, `{"pid":80667,"ppid":1,"path":"/usr/libexec/sshd-session","pidversion":1888041,"parent_pidversion":1,`+
			`"group_id":80667}`),
		lineageEvt("f-login", "fork", 910, `{"child_pid":80671,"parent_pid":80667,"pidversion":1888058,"parent_pidversion":1888041}`),
		lineageEvt("e-login", "exec", 915, `{"pid":80671,"ppid":80667,"path":"/bin/zsh","pidversion":1888059,"parent_pidversion":1888041,`+
			`"group_id":80671}`),
		lineageEvt("f-claude", "fork", 1000, `{"child_pid":80673,"parent_pid":80671,"pidversion":1888064,"parent_pidversion":1888059}`),
		lineageEvt("e-claude", "exec", 1010, `{"pid":80673,"ppid":80671,"path":"/opt/claude/2.1.296","pidversion":1888065,`+
			`"parent_pidversion":1888059,"group_id":80671}`),
		lineageEvt("f-rg", "fork", 1020, `{"child_pid":80693,"parent_pid":80673,"pidversion":1888113,"parent_pidversion":1888065}`),
		lineageEvt("e-rg", "exec", 1030, `{"pid":80693,"ppid":80673,"path":"/opt/rg","pidversion":1888114,"parent_pidversion":1888065,`+
			`"group_id":80671,"env":{"AI_AGENT":"claude-code_2-1-296_harness"}}`),
		// Tool call one: (sleep 47 &), a double fork launchd adopts, and a perl that calls setsid into its own group.
		lineageEvt("f-call", "fork", 2000, `{"child_pid":80730,"parent_pid":80673,"pidversion":1888195,"parent_pidversion":1888065}`),
		lineageEvt("e-call", "exec", 2010, `{"pid":80730,"ppid":80673,"path":"/bin/zsh","pidversion":1888196,"parent_pidversion":1888065,`+
			`"group_id":80730,`+toolCallEnv(80673)+`}`),
		lineageEvt("f-sub", "fork", 2020, `{"child_pid":80731,"parent_pid":80730,"pidversion":1888197,"parent_pidversion":1888196}`),
		lineageEvt("f-daemon", "fork", 2030, `{"child_pid":80732,"parent_pid":80731,"pidversion":1888198,"parent_pidversion":1888197}`),
		lineageEvt("x-sub", "exit", 2040, `{"pid":80731,"exit_code":0,"pidversion":1888197}`),
		lineageEvt("e-daemon", "exec", 2050, `{"pid":80732,"ppid":1,"original_ppid":80731,"path":"/bin/sleep","pidversion":1888199,`+
			`"parent_pidversion":7,"group_id":80730,`+toolCallEnv(80673)+`}`),
		lineageEvt("f-perl", "fork", 2100, `{"child_pid":80740,"parent_pid":80730,"pidversion":1888210,"parent_pidversion":1888196}`),
		lineageEvt("e-perl", "exec", 2110, `{"pid":80740,"ppid":80730,"path":"/usr/bin/perl","pidversion":1888211,"parent_pidversion":1888196,`+
			`"group_id":80740,`+toolCallEnv(80673)+`}`),
		// Tool call two.
		lineageEvt("f-call2", "fork", 3000, `{"child_pid":80838,"parent_pid":80673,"pidversion":1888358,"parent_pidversion":1888065}`),
		lineageEvt("e-call2", "exec", 3010, `{"pid":80838,"ppid":80673,"path":"/bin/zsh","pidversion":1888359,"parent_pidversion":1888065,`+
			`"group_id":80838,`+toolCallEnv(80673)+`}`),
		// The tool call's shell execs its command in place.
		lineageEvt("e-call2-ls", "exec", 3020, `{"pid":80838,"ppid":80673,"path":"/bin/ls","pidversion":1888360,"parent_pidversion":1888065,`+
			`"group_id":80838,`+toolCallEnv(80673)+`}`),
		// Tool call three: its own shell calls setsid into a new group and then re-execs, still naming the agent.
		lineageEvt("f-call3", "fork", 3500, `{"child_pid":80850,"parent_pid":80673,"pidversion":1888400,"parent_pidversion":1888065}`),
		lineageEvt("e-call3", "exec", 3510, `{"pid":80850,"ppid":80673,"path":"/bin/zsh","pidversion":1888401,"parent_pidversion":1888065,`+
			`"group_id":80850,`+toolCallEnv(80673)+`}`),
		lineageEvt("e-call3-setsid", "exec", 3520, `{"pid":80850,"ppid":80673,"path":"/usr/bin/perl","pidversion":1888402,`+
			`"parent_pidversion":1888065,"group_id":80850,"session_id":80850,`+toolCallEnv(80673)+`}`),
		lineageEvt("e-call3-again", "exec", 3530, `{"pid":80850,"ppid":80673,"path":"/bin/sleep","pidversion":1888403,`+
			`"parent_pidversion":1888065,"group_id":80999,`+toolCallEnv(80673)+`}`),
		// A process carrying the markers whose creator, not the agent, was never seen: it does not start a tool call.
		lineageEvt("e-lost", "exec", 3600, `{"pid":80990,"ppid":80989,"path":"/bin/cat","pidversion":1888500,"parent_pidversion":1888499,`+
			`"group_id":80990,`+toolCallEnv(80673)+`}`),
		// Roots of their own: a daemon launchd forked, and a process first seen at exec under launchd. A reparented process first
		// seen at exec names no creator, so it has no root.
		lineageEvt("f-agentd", "fork", 4000, `{"child_pid":90000,"parent_pid":1,"pidversion":1889000,"parent_pidversion":1}`),
		lineageEvt("s-snap", "exec", 4100, `{"pid":91000,"ppid":1,"original_ppid":1,"path":"/usr/libexec/syslogd","snapshot":true,`+
			`"pidversion":1889100,"parent_pidversion":1}`),
		// A real startup snapshot reports no original_ppid, so a daemon under launchd in it may have been adopted, not started.
		lineageEvt("s-snap-real", "exec", 4150, `{"pid":91500,"ppid":1,"path":"/usr/sbin/adopted","snapshot":true,"pidversion":1889150,`+
			`"parent_pidversion":1,"group_id":91400}`),
		lineageEvt("e-orphan", "exec", 4200, `{"pid":92000,"ppid":1,"original_ppid":640,"path":"/opt/orphan","pidversion":1889200,`+
			`"parent_pidversion":7}`),
	}
}

// rootPID returns the pid of the row r's story root, or 0 when it has none.
func rootPID(rows []normRow, r normRow) int {
	if r.StoryRootRank < 0 {
		return 0
	}
	return rows[r.StoryRootRank].PID
}

func toolCallOf(r normRow) int {
	if r.ToolCallGroup == nil {
		return 0
	}
	return *r.ToolCallGroup
}

// spec:server-process-graph-builder/story-root-and-tool-call-stamping/a-story-reaches-the-process-launchd-started
// spec:server-process-graph-builder/story-root-and-tool-call-stamping/a-process-launchd-started-is-its-own-story-root
// spec:server-process-graph-builder/story-root-and-tool-call-stamping/a-tool-call-is-keyed-by-the-agent-child-s-process-group
// spec:server-process-graph-builder/story-root-and-tool-call-stamping/a-tool-call-survives-daemonizing-and-setsid
func TestStoryRootAndToolCall(t *testing.T) {
	t.Parallel()
	rows := processInBatches(t, storyScenario())

	t.Run("every descendant of sshd-session belongs to its story", func(t *testing.T) {
		t.Parallel()
		for _, r := range []normRow{
			generationAt(t, rows, 80667, 905), generationAt(t, rows, 80673, 1010), generationAt(t, rows, 80730, 2010),
			generationAt(t, rows, 80732, 2050), generationAt(t, rows, 80740, 2110), forkAt(t, rows, 80731, 2020),
			generationAt(t, rows, 80838, 3020),
		} {
			assert.Equal(t, 80667, rootPID(rows, r), "pid %d", r.PID)
		}
	})

	t.Run("launchd's children are their own roots", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 90000, rootPID(rows, forkAt(t, rows, 90000, 4000)))
		assert.Equal(t, 91000, rootPID(rows, generationAt(t, rows, 91000, 4100)), "first seen at exec under launchd")
		assert.Zero(t, rootPID(rows, generationAt(t, rows, 92000, 4200)), "reparented, so its creator is unknown")
		assert.Zero(t, rootPID(rows, generationAt(t, rows, 91500, 4150)), "a snapshot that does not say launchd started it")
	})

	t.Run("tool calls are keyed by the agent child's group", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 80730, toolCallOf(generationAt(t, rows, 80730, 2010)))
		assert.Equal(t, 80838, toolCallOf(generationAt(t, rows, 80838, 3010)))
		assert.Equal(t, 80838, toolCallOf(generationAt(t, rows, 80838, 3020)), "its in-place exec")
		assert.Equal(t, 80850, toolCallOf(generationAt(t, rows, 80850, 3530)), "a re-exec after its own shell changed groups")
		assert.Equal(t, 80730, toolCallOf(generationAt(t, rows, 80732, 2050)), "a daemon launchd adopted")
		assert.Equal(t, 80730, toolCallOf(generationAt(t, rows, 80740, 2110)), "a process that called setsid")
		assert.Zero(t, toolCallOf(generationAt(t, rows, 80673, 1010)), "the agent process")
		assert.Zero(t, toolCallOf(generationAt(t, rows, 80990, 3600)), "a marked process the agent did not create")
		assert.Zero(t, toolCallOf(generationAt(t, rows, 80693, 1030)), "a harness helper")
	})

	t.Run("one event per batch matches one batch", func(t *testing.T) {
		t.Parallel()
		bRef, bBatch, db := twoBuilders(t)
		requireBatchInvariant(t, db, bRef, bBatch, storyScenario(), "story-"+t.Name())
	})
}

// Property: in any fork forest, each process's story root is its nearest ancestor, itself included, that launchd forked. Children
// name their creator's current generation, as the kernel does, so every link resolves.
func TestStoryRoot_NearestLaunchdChildProperty(t *testing.T) {
	t.Parallel()
	b, _, db := twoBuilders(t)
	iter := 0
	rapid.Check(t, func(rt *rapid.T) {
		iter++
		n := rapid.IntRange(1, 20).Draw(rt, "n")
		want := make([]int, n)
		version := uint32(5000)
		current := map[int]uint32{1: 1}
		var events []api.Event
		for i := range n {
			is := strconv.Itoa(i)
			pid := 3000 + i
			parentPID := 1
			if i > 0 && rapid.Bool().Draw(rt, "child"+is) {
				parentPID = 3000 + rapid.IntRange(0, i-1).Draw(rt, "parent"+is)
			}
			want[i] = pid
			if parentPID != 1 {
				want[i] = want[parentPID-3000]
			}
			version++
			ts := int64(20_000 + i*100)
			events = append(events, lineageEvt("f"+is, "fork", ts, `{"child_pid":`+strconv.Itoa(pid)+`,"parent_pid":`+
				strconv.Itoa(parentPID)+`,"pidversion":`+strconv.Itoa(int(version))+`,"parent_pidversion":`+
				strconv.Itoa(int(current[parentPID]))+`}`))
			current[pid] = version
		}
		host := "story-pbt-" + strconv.Itoa(iter)
		if err := b.ProcessBatch(t.Context(), rewriteHost(events, host)); err != nil {
			rt.Fatal(err)
		}
		rows := dumpNormalizedForest(t, db, t.Context(), host)
		for i := range n {
			assert.Equal(rt, want[i], rootPID(rows, forkAt(t, rows, 3000+i, int64(20_000+i*100))), "process %d", i)
		}
	})
}

// The exec-chain and process-search reads return the stamped keys with the row, as the tree reads do.
func TestStoryRoot_ReadsReturnTheKeys(t *testing.T) {
	t.Parallel()
	store, _ := openProcessStore(t)
	b := graph.NewBuilder(store, discardLogger())
	host := "story-reads-" + t.Name()
	require.NoError(t, b.ProcessBatch(t.Context(), rewriteHost(storyScenario(), host)))

	current, err := store.GetProcessByPID(t.Context(), host, 80838, 3020)
	require.NoError(t, err)
	require.NotNil(t, current)
	chain, err := store.GetExecChain(t.Context(), *current)
	require.NoError(t, err)
	require.Len(t, chain, 1)
	assert.Equal(t, new(80838), chain[0].ToolCallGroup)
	assert.NotNil(t, chain[0].StoryRootID)
	assert.Equal(t, new(sessionOne), chain[0].AgentSessionID)

	res, err := store.SearchProcesses(t.Context(), api.ProcessSearchFilter{HostID: host, Path: "/bin/sleep"}, "", 10)
	require.NoError(t, err)
	require.NotEmpty(t, res.Rows)
	for _, r := range res.Rows {
		assert.NotNil(t, r.ToolCallGroup, "pid %d", r.PID)
		assert.Equal(t, current.StoryRootID, r.StoryRootID, "pid %d", r.PID)
	}
}
