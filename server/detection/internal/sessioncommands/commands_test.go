package sessioncommands

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

const ms = int64(1_000_000)

// proc is one process row as the session's forest read returns it: a fork that never exec'd (nil args) carries no exec time and no
// user, and everything the capture exec'd ran as uid 501 and exited 5 ms after its fork.
func proc(id int64, pid int, atMs int64, path string, args []string, children ...api.ProcessNode) api.ProcessNode {
	p := api.ProcessNode{Process: api.Process{ID: id, PID: pid, Path: path, ForkTimeNs: atMs * ms}, Children: children}
	if args != nil {
		raw, _ := json.Marshal(args)
		p.Args = raw
		p.ExecTimeNs = new(atMs*ms + 1)
		p.UID = new(501)
	}
	p.ExitTimeNs, p.ExitReason = new(atMs*ms+5*ms), new(api.ExitReasonEvent)
	return p
}

// keyed sets a row's tool-call group.
func keyed(p api.ProcessNode, group int) api.ProcessNode {
	p.ToolCallGroup = &group
	return p
}

// wrapper is the shell Claude Code runs one Bash tool call in, as captured on edr-dev, leading its own process group.
func wrapper(id int64, pid int, atMs int64, quotedCommand string, children ...api.ProcessNode) api.ProcessNode {
	script := "source /Users/victor/.claude/shell-snapshots/snapshot-zsh-1791902227385-pju9os.sh 2>/dev/null || true && " +
		"eval " + quotedCommand + " < /dev/null && pwd -P >| /tmp/claude-786b-cwd"
	return keyed(proc(id, pid, atMs, "/bin/zsh", []string{"/bin/zsh", "-c", script}, children...), pid)
}

// realSession is the forest the server returns for the session captured on edr-dev (Claude Code 2.1.296): five Bash tool calls, each
// a root because the agent process itself carries no session marker. Row ids, pids and shapes are the capture's; times are relative.
// The roots arrive out of order, as a forest read gives no order guarantee.
func realSession() []api.ProcessNode {
	reexec := func(p api.ProcessNode) api.ProcessNode {
		p.ExitReason = new(api.ExitReasonReExec)
		return p
	}
	daemon := proc(2298441, 80839, 302, "/bin/sleep", []string{"sleep", "48"})
	daemon.ExitTimeNs = new(350_000 * ms)
	return []api.ProcessNode{
		wrapper(2298439, 80838, 300, `'nohup sleep 48 >/dev/null 2>&1 &'`,
			reexec(proc(2298440, 80839, 301, "/usr/bin/nohup", []string{"nohup", "sleep", "48"})), daemon),
		wrapper(2298322, 80727, 100, `'ls /private/tmp | head -3'`,
			proc(2298324, 80729, 102, "/usr/bin/head", []string{"head", "-3"}),
			proc(2298323, 80728, 101, "/bin/ls", []string{"ls", "/private/tmp"})),
		wrapper(2298325, 80730, 200, `'(sleep 47 &)'`,
			// The subshell's fork of zsh, which never exec'd, then the sleep it backgrounded and that outlived the call.
			proc(2298326, 80731, 201, "/bin/zsh", nil, proc(2298327, 80732, 202, "/bin/sleep", []string{"sleep", "47"}))),
		wrapper(2298442, 80840, 400, `'env -i /bin/echo qa1280'`,
			reexec(proc(2298444, 80842, 401, "/usr/bin/env", []string{"env", "-i", "/bin/echo", "qa1280"})),
			proc(2298445, 80842, 402, "/bin/echo", []string{"/bin/echo", "qa1280"})),
		wrapper(2298447, 80843, 500, `'/bin/sh -c "sleep 1; echo qa1280-done"'`,
			reexec(proc(2298448, 80844, 501, "/bin/sh", []string{"/bin/sh", "-c", "sleep 1; echo qa1280-done"})),
			proc(2298449, 80844, 502, "/bin/bash", []string{"/bin/sh", "-c", "sleep 1; echo qa1280-done"},
				proc(2298450, 80845, 503, "/bin/sleep", []string{"sleep", "1"}))),
	}
}

func labels(commands []Command) []string {
	out := []string{}
	for _, c := range commands {
		out = append(out, c.Command)
	}
	return out
}

// spec:server-rest-api/agent-session-commands/commands-are-labelled-and-paired-with-their-tool-calls
func TestBuild_OneCommandPerToolCall(t *testing.T) {
	t.Parallel()
	commands := Build(realSession())
	assert.Equal(t, []string{
		"ls /private/tmp | head -3", "(sleep 47 &)", "nohup sleep 48 >/dev/null 2>&1 &", "env -i /bin/echo qa1280",
		`/bin/sh -c "sleep 1; echo qa1280-done"`,
	}, labels(commands), "in the order they ran, labelled with the agent's command")
	keys := []string{}
	for _, c := range commands {
		keys = append(keys, c.Key)
		assert.True(t, c.FromAgent)
		assert.Equal(t, new(501), c.UID)
	}
	assert.Equal(t, []string{"row:2298322", "row:2298325", "row:2298439", "row:2298442", "row:2298447"}, keys)
	assert.Equal(t, []int64{2298325, 2298326, 2298327}, commands[1].ProcessIDs, "every process, plumbing included")
	assert.Equal(t, 350_000*ms, commands[2].LastNs, "the daemon outlived its call, and the span reaches its exit")
	assert.Equal(t, 100*ms, commands[0].StartNs)
}

func TestBuild_SynthesizedExitDoesNotStretchTheSpan(t *testing.T) {
	t.Parallel()
	daemon := proc(3, 3, 202, "/bin/sleep", []string{"sleep", "47"})
	daemon.ExitReason, daemon.ExitTimeNs = new(api.ExitReasonTTLReconciliation), new(9_000_000*ms)
	root := keyed(proc(2, 2, 200, "/bin/zsh", []string{"zsh", "-c", "eval '(sleep 47 &)'"}, daemon), 2)
	assert.Equal(t, 205*ms, Build([]api.ProcessNode{root})[0].LastNs, "the shell's observed exit, not the sweep's")
}

func TestBuild_Roots(t *testing.T) {
	t.Parallel()

	t.Run("a root whose creator was not read joins its tool call", func(t *testing.T) {
		t.Parallel()
		orphan := keyed(proc(9, 9, 150, "/usr/bin/git", []string{"git", "status"}), 80727)
		commands := Build(append(realSession(), orphan))
		require.Len(t, commands, 5)
		assert.Len(t, commands[0].Roots, 2)
		assert.Equal(t, []int64{2298322, 2298324, 2298323, 9}, commands[0].ProcessIDs)
	})

	t.Run("a later leader reusing a process group starts a command of its own", func(t *testing.T) {
		t.Parallel()
		first := keyed(proc(1, 700, 100, "/bin/zsh", []string{"/bin/zsh", "-c", "eval 'git status'"}), 700)
		reused := keyed(proc(2, 700, 900, "/bin/zsh", []string{"/bin/zsh", "-c", "eval 'make test'"}), 700)
		// A process of the later call whose creator was not read joins the later call, not the earlier one.
		orphan := keyed(proc(3, 701, 950, "/usr/bin/make", []string{"make", "test"}), 700)
		commands := Build([]api.ProcessNode{orphan, reused, first})
		assert.Equal(t, []string{"git status", "make test"}, labels(commands))
		assert.Equal(t, []int64{2, 3}, commands[1].ProcessIDs)
	})

	t.Run("a root without a tool-call group is a command of its own, labelled with its command line", func(t *testing.T) {
		t.Parallel()
		hook := proc(7, 7, 50, "/usr/bin/osascript", []string{"osascript", "-e", "beep"})
		commands := Build(append([]api.ProcessNode{hook}, realSession()...))
		assert.Equal(t, "row:7", commands[0].Key)
		assert.Equal(t, "osascript -e beep", commands[0].Command)
		assert.False(t, commands[0].FromAgent)
	})

	t.Run("a fork that never exec'd is labelled with its path", func(t *testing.T) {
		t.Parallel()
		commands := Build([]api.ProcessNode{proc(4, 4, 10, "/bin/zsh", nil)})
		assert.Equal(t, "/bin/zsh", commands[0].Command)
		assert.Nil(t, commands[0].UID)
	})

	t.Run("a call that execs keeps its shell's re-exec in its command", func(t *testing.T) {
		t.Parallel()
		shell := keyed(proc(1, 500, 100, "/bin/zsh", []string{"/bin/zsh", "-c", "eval 'exec /bin/ls'"}), 500)
		shell.ExitReason = new(api.ExitReasonReExec)
		// The re-exec keeps the shell's pid, fork time, creator and group, so it arrives as a root leading the same group.
		ls := keyed(proc(2, 500, 100, "/bin/ls", []string{"/bin/ls"}), 500)
		ls.PreviousExecID = new(int64(1))
		// A later call whose leader reuses the pid is still a command of its own.
		reused := keyed(proc(3, 500, 900, "/bin/zsh", []string{"/bin/zsh", "-c", "eval 'pwd'"}), 500)
		commands := Build([]api.ProcessNode{reused, ls, shell})
		assert.Equal(t, []string{"exec /bin/ls", "pwd"}, labels(commands))
		assert.Equal(t, []int64{1, 2}, commands[0].ProcessIDs)
	})

	t.Run("an empty session has no commands", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []Command{}, Build(nil))
	})
}

func TestApplyLaunchLinks(t *testing.T) {
	t.Parallel()
	opener := keyed(proc(1, 100, 10, "/bin/zsh", []string{"/bin/zsh", "-c", "eval 'open -a TextEdit'"},
		proc(2, 101, 11, "/usr/bin/open", []string{"open", "-a", "TextEdit"})), 100)
	textEdit := proc(3, 200, 12, "/System/Applications/TextEdit.app/Contents/MacOS/TextEdit", []string{"TextEdit"},
		proc(5, 202, 14, "/usr/bin/helper", []string{"helper"}))
	service := proc(4, 201, 13, "/usr/libexec/service", []string{"service"})
	roots := []api.ProcessNode{opener, textEdit, service}

	got := ApplyLaunchLinks(roots, []api.ProcessLink{
		{From: 2, To: 3, Kind: api.ProcessLinkInferredLaunch},
		{From: 3, To: 4, Kind: api.ProcessLinkResponsibleProcess},
		// Ends that are not both in the forest, and a move that would put a process under its own descendant, are left alone.
		{From: 2, To: 99, Kind: api.ProcessLinkInferredLaunch},
		{From: 5, To: 3, Kind: api.ProcessLinkInferredLaunch},
	})
	require.Len(t, got.Roots, 1, "the launched app and its service hang under the command that opened it")
	open := got.Roots[0].Children[0]
	require.Len(t, open.Children, 1)
	assert.Equal(t, int64(3), open.Children[0].ID)
	assert.Equal(t, []int64{4, 5}, []int64{open.Children[0].Children[0].ID, open.Children[0].Children[1].ID}, "in fork order")
	assert.Equal(t, map[int64]bool{3: true}, got.Inferred)
	assert.Equal(t, map[int64]bool{4: true}, got.Services)
	assert.Len(t, roots, 3, "the caller's forest is not changed")
	assert.Empty(t, roots[0].Children[0].Children, "the caller's forest is not changed")

	assert.Equal(t, []api.ProcessNode{}, ApplyLaunchLinks(nil, nil).Roots)
}
