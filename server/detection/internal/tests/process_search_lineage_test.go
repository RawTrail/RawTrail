//go:build integration

package tests

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/bootstrap"
)

// lineageSeed is a process row with the keys the agent, story, ancestor and argument filters read (issue #1289).
type lineageSeed struct {
	pid        int
	forkNs     int64
	path, args string
	session    string
	agent      string
	story      *int64
	parent     *int64
	previous   *int64
}

// seedLineageProcess inserts one row and returns its id.
func seedLineageProcess(t *testing.T, ctx context.Context, d *bootstrap.Detection, host string, seed lineageSeed) int64 {
	t.Helper()
	var session, agent any
	if seed.session != "" {
		session, agent = seed.session, seed.agent
	}
	res, err := d.Store().DB().ExecContext(ctx, `
		INSERT INTO processes (host_id, pid, ppid, path, args, uid, fork_time_ns, exec_time_ns, agent_session_id, agent_name,
			story_root_id, parent_process_id, previous_exec_id)
		VALUES (?, ?, 1, ?, ?, 501, ?, ?, ?, ?, ?, ?, ?)`,
		host, seed.pid, seed.path, seed.args, seed.forkNs, seed.forkNs+1, session, agent, seed.story, seed.parent, seed.previous)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return id
}

// spec:server-rest-api/process-search-by-lineage/a-search-keeps-a-session-a-story-or-a-process-s-descendants
// spec:server-rest-api/process-search-by-lineage/arguments-are-matched-one-at-a-time
func TestStore_SearchProcesses_LineageFilters(t *testing.T) {
	t.Parallel()
	d := newDetection(t, detectionOpts{mode: bootstrap.ModeFull})
	ctx := t.Context()
	const host = "SRCH-LINEAGE"
	require.NoError(t, d.Service().RecordHostSeen(ctx, host, time.Now()))

	// The story: the terminal launchd started (root), a Claude Code tool call's shell under it, its curl, and the curl's re-exec.
	root := seedLineageProcess(t, ctx, d, host, lineageSeed{pid: 500, forkNs: 1000, path: "/Applications/iTerm.app/iTerm2",
		args: `["iTerm2"]`})
	_, err := d.Store().DB().ExecContext(ctx, "UPDATE processes SET story_root_id = id WHERE id = ?", root)
	require.NoError(t, err)
	shell := seedLineageProcess(t, ctx, d, host, lineageSeed{pid: 501, forkNs: 2000, path: "/bin/zsh",
		args: `["/bin/zsh","-c","eval 'curl -s https://example.com'"]`, session: "s1", agent: "claude-code", story: &root, parent: &root})
	curl := seedLineageProcess(t, ctx, d, host, lineageSeed{pid: 502, forkNs: 3000, path: "/usr/bin/curl",
		args: `["curl","-s","https://example.com"]`, session: "s1", agent: "claude-code", story: &root, parent: &shell})
	// A re-exec keeps its prior generation's creator, as the graph builder writes it, so it hangs from the shell twice over.
	reexec := seedLineageProcess(t, ctx, d, host, lineageSeed{pid: 502, forkNs: 3000, path: "/usr/bin/true", args: `["true","100%"]`,
		session: "s1", agent: "claude-code", story: &root, parent: &shell, previous: &curl})
	// Elsewhere: another Claude Code session's curl, a Codex session's curl, and a curl no agent ran.
	other := seedLineageProcess(t, ctx, d, host, lineageSeed{pid: 600, forkNs: 4000, path: "/usr/bin/curl",
		args: `["curl","https://fleetdm.com"]`, session: "s2", agent: "claude-code"})
	codex := seedLineageProcess(t, ctx, d, host, lineageSeed{pid: 700, forkNs: 5000, path: "/usr/bin/curl",
		args: `["curl","https://example.com"]`, session: "s3", agent: "codex"})
	plain := seedLineageProcess(t, ctx, d, host, lineageSeed{pid: 800, forkNs: 6000, path: "/usr/bin/curl", args: `["curl","1000"]`})

	cases := []struct {
		name   string
		filter api.ProcessSearchFilter
		want   []int64
	}{
		{"one session", api.ProcessSearchFilter{AgentSession: "s1"}, []int64{shell, curl, reexec}},
		{"every curl any Claude Code session ran", api.ProcessSearchFilter{Agent: "claude-code", Path: "curl"}, []int64{curl, other}},
		{"one story", api.ProcessSearchFilter{StoryRoot: root}, []int64{root, shell, curl, reexec}},
		{"below a tool call's shell, its re-exec included", api.ProcessSearchFilter{Ancestor: shell}, []int64{curl, reexec}},
		{"below the story's root", api.ProcessSearchFilter{Ancestor: root}, []int64{shell, curl, reexec}},
		{"below a process with nothing below it", api.ProcessSearchFilter{Ancestor: plain}, nil},
		{"an argument holds the text", api.ProcessSearchFilter{Args: "example.com"}, []int64{shell, curl, codex}},
		{"arguments are matched case-sensitively", api.ProcessSearchFilter{Args: "EXAMPLE.COM"}, nil},
		{"an argument's percent sign is literal", api.ProcessSearchFilter{Args: "100%"}, []int64{reexec}},
		{"text across two arguments is not one argument's", api.ProcessSearchFilter{Args: "curl -s"}, []int64{shell}},
		{"filters compose", api.ProcessSearchFilter{Agent: "claude-code", Args: "example.com", Ancestor: root}, []int64{shell, curl}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.filter.HostID = host
			res, err := d.Store().SearchProcesses(context.Background(), tc.filter, "", 50)
			require.NoError(t, err)
			var got []int64
			for _, r := range res.Rows {
				got = append(got, r.ID)
			}
			assert.ElementsMatch(t, tc.want, got)
			assert.EqualValues(t, len(tc.want), res.TotalMatched)
		})
	}
}

// A chain deeper than MySQL's recursion limit (a fork chain a runaway script built) is walked to the bound, not failed.
//
// spec:server-rest-api/process-search-by-lineage/a-deep-chain-is-walked-to-its-bound
func TestStore_SearchProcesses_DeepAncestorChain(t *testing.T) {
	t.Parallel()
	d := newDetection(t, detectionOpts{mode: bootstrap.ModeFull})
	ctx := t.Context()
	const host = "SRCH-DEEP"
	require.NoError(t, d.Service().RecordHostSeen(ctx, host, time.Now()))
	top := seedLineageProcess(t, ctx, d, host, lineageSeed{pid: 1, forkNs: 1, path: "/bin/sh", args: `["sh"]`})
	parent := top
	for i := range 1100 {
		parent = seedLineageProcess(t, ctx, d, host, lineageSeed{pid: 2 + i, forkNs: int64(2 + i), path: "/bin/sh", args: `["sh"]`,
			parent: &parent})
	}
	res, err := d.Store().SearchProcesses(ctx, api.ProcessSearchFilter{HostID: host, Ancestor: top}, "", 1)
	require.NoError(t, err)
	assert.EqualValues(t, 900, res.TotalMatched, "walked to the depth bound")
}

// spec:server-rest-api/process-search-by-lineage/a-process-s-descendants-are-each-listed-once
func TestStore_SearchProcesses_AncestorWalk(t *testing.T) {
	t.Parallel()
	d := newDetection(t, detectionOpts{mode: bootstrap.ModeFull})
	ctx := t.Context()
	const host = "SRCH-WALK"
	require.NoError(t, d.Service().RecordHostSeen(ctx, host, time.Now()))

	t.Run("generations that re-exec and fork are each listed once", func(t *testing.T) {
		// A shell re-execs three times, as a script that execs a chain of wrappers does; each generation forks a child. Each
		// re-exec row keeps the shell's creator, so following both edges would reach the later generations once per earlier one.
		top := seedLineageProcess(t, ctx, d, host, lineageSeed{pid: 10, forkNs: 10, path: "/bin/zsh", args: `["zsh"]`})
		want := []int64{}
		gen := seedLineageProcess(t, ctx, d, host, lineageSeed{pid: 11, forkNs: 11, path: "/bin/sh", args: `["sh"]`, parent: &top})
		want = append(want, gen)
		for i := range 3 {
			child := seedLineageProcess(t, ctx, d, host, lineageSeed{pid: 20 + i, forkNs: int64(20 + i), path: "/bin/ls",
				args: `["ls"]`, parent: &gen})
			next := seedLineageProcess(t, ctx, d, host, lineageSeed{pid: 11, forkNs: 11, path: "/bin/sh", args: `["sh"]`, parent: &top,
				previous: &gen})
			want = append(want, child, next)
			gen = next
		}
		res, err := d.Store().SearchProcesses(ctx, api.ProcessSearchFilter{HostID: host, Ancestor: top}, "", 50)
		require.NoError(t, err)
		var got []int64
		for _, r := range res.Rows {
			got = append(got, r.ID)
		}
		assert.ElementsMatch(t, want, got)
	})

	t.Run("an ancestor with no story reaches a descendant that has one", func(t *testing.T) {
		top := seedLineageProcess(t, ctx, d, host, lineageSeed{pid: 30, forkNs: 30, path: "/bin/zsh", args: `["zsh"]`})
		story := top + 1000
		child := seedLineageProcess(t, ctx, d, host, lineageSeed{pid: 31, forkNs: 31, path: "/bin/ls", args: `["ls"]`, parent: &top,
			story: &story})
		res, err := d.Store().SearchProcesses(ctx, api.ProcessSearchFilter{HostID: host, Ancestor: top}, "", 50)
		require.NoError(t, err)
		require.Len(t, res.Rows, 1)
		assert.Equal(t, child, res.Rows[0].ID)
	})
}
