//go:build integration

// Agent session stamping (issue #1281). An exec carrying Claude Code's session marker stamps its row; every other row inherits its
// creator's session, so env-stripped and daemonized descendants stay attributed. The scenario follows a real Claude Code 2.1.296
// session captured on edr-dev: the claude process itself carries no session marker, each Bash tool call is a zsh that does, and
// Claude Code's own helpers carry AI_AGENT with a harness role but no session.

package tests

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/fleetdm/edr/server/detection/api"
)

const (
	sessionOne = "169b5bb1-29b5-41da-9a54-92cd72599ad9"
	sessionTwo = "2c7d3e10-9f4a-4b8e-a1d2-0e5f6a7b8c9d"
)

// markers renders an exec's env with Claude Code's tool-call markers for session.
func markers(session string) string {
	return `"env":{"CLAUDE_CODE_SESSION_ID":"` + session + `","AI_AGENT":"claude-code_2-1-296_agent","CLAUDECODE":"1"}`
}

func agentSessionScenario() []api.Event {
	return []api.Event{
		lineageEvt("f-claude", "fork", 1000, `{"child_pid":80673,"parent_pid":80671,"pidversion":1888064,"parent_pidversion":1888059}`),
		lineageEvt("e-claude", "exec", 1010, `{"pid":80673,"ppid":80671,"path":"/Users/v/.local/share/claude/versions/2.1.296",`+
			`"pidversion":1888065,"parent_pidversion":1888059}`),
		// A helper Claude Code runs for itself: it names the agent but belongs to no session.
		lineageEvt("f-rg", "fork", 1020, `{"child_pid":80693,"parent_pid":80673,"pidversion":1888113,"parent_pidversion":1888065}`),
		lineageEvt("e-rg", "exec", 1030, `{"pid":80693,"ppid":80673,"path":"/opt/rg","pidversion":1888114,"parent_pidversion":1888065,`+
			`"env":{"AI_AGENT":"claude-code_2-1-296_harness"}}`),
		// Tool call one: (sleep 47 &), a subshell that double-forks a daemon launchd adopts; the daemon's exec carries no markers.
		lineageEvt("f-call", "fork", 2000, `{"child_pid":80730,"parent_pid":80673,"pidversion":1888195,"parent_pidversion":1888065}`),
		lineageEvt("e-call", "exec", 2010, `{"pid":80730,"ppid":80673,"path":"/bin/zsh","pidversion":1888196,"parent_pidversion":1888065,`+
			markers(sessionOne)+`}`),
		lineageEvt("f-sub", "fork", 2020, `{"child_pid":80731,"parent_pid":80730,"pidversion":1888197,"parent_pidversion":1888196}`),
		lineageEvt("f-daemon", "fork", 2030, `{"child_pid":80732,"parent_pid":80731,"pidversion":1888198,"parent_pidversion":1888197}`),
		lineageEvt("x-sub", "exit", 2040, `{"pid":80731,"exit_code":0,"pidversion":1888197}`),
		lineageEvt("e-daemon", "exec", 2050, `{"pid":80732,"ppid":1,"original_ppid":80731,"path":"/bin/sleep","pidversion":1888199,`+
			`"parent_pidversion":7}`),
		// Tool call two: env -i strips the environment, then execs echo in place.
		lineageEvt("f-call2", "fork", 3000, `{"child_pid":80840,"parent_pid":80673,"pidversion":1888363,"parent_pidversion":1888065}`),
		lineageEvt("e-call2", "exec", 3010, `{"pid":80840,"ppid":80673,"path":"/bin/zsh","pidversion":1888364,"parent_pidversion":1888065,`+
			markers(sessionOne)+`}`),
		lineageEvt("f-env", "fork", 3020, `{"child_pid":80842,"parent_pid":80840,"pidversion":1888367,"parent_pidversion":1888364}`),
		lineageEvt("e-env", "exec", 3030, `{"pid":80842,"ppid":80840,"path":"/usr/bin/env","pidversion":1888368,"parent_pidversion":1888364,`+
			markers(sessionOne)+`}`),
		lineageEvt("e-echo", "exec", 3040, `{"pid":80842,"ppid":80840,"path":"/bin/echo","pidversion":1888369,"parent_pidversion":1888364}`),
		// After /clear: a tool call in a new session.
		lineageEvt("f-call3", "fork", 4000, `{"child_pid":80900,"parent_pid":80673,"pidversion":1888500,"parent_pidversion":1888065}`),
		lineageEvt("e-call3", "exec", 4010, `{"pid":80900,"ppid":80673,"path":"/bin/zsh","pidversion":1888501,"parent_pidversion":1888065,`+
			markers(sessionTwo)+`}`),
		lineageEvt("f-ls", "fork", 4020, `{"child_pid":80901,"parent_pid":80900,"pidversion":1888502,"parent_pidversion":1888501}`),
		// A process first seen at exec, its fork lost, under tool call one's shell: it inherits from the creator its exec names.
		lineageEvt("e-orphan", "exec", 2060, `{"pid":80960,"ppid":80730,"path":"/bin/cat","pidversion":1888210,`+
			`"parent_pidversion":1888196}`),
		// A marker longer than the 256 bytes a row stores.
		lineageEvt("f-call4", "fork", 5000, `{"child_pid":80950,"parent_pid":80673,"pidversion":1888600,"parent_pidversion":1888065}`),
		lineageEvt("e-call4", "exec", 5010, `{"pid":80950,"ppid":80673,"path":"/bin/zsh","pidversion":1888601,"parent_pidversion":1888065,`+
			markers(strings.Repeat("x", 300))+`}`),
	}
}

func sessionOf(r normRow) string {
	if r.AgentSessionID == nil {
		return ""
	}
	return *r.AgentSessionID
}

// spec:server-process-graph-builder/agent-session-stamping/a-tool-call-stamps-its-session
// spec:server-process-graph-builder/agent-session-stamping/a-child-stripped-of-its-environment-keeps-the-session
// spec:server-process-graph-builder/agent-session-stamping/a-new-marker-starts-a-new-session
// spec:server-process-graph-builder/agent-session-stamping/a-process-outside-any-agent-has-no-session
func TestAgentSession_StampedAndInherited(t *testing.T) {
	t.Parallel()
	rows := processInBatches(t, agentSessionScenario())

	call := generationAt(t, rows, 80730, 2010)
	assert.Equal(t, api.AgentSession{AgentSessionID: new(sessionOne), AgentName: new("claude-code"), AgentVersion: new("2.1.296")},
		call.AgentSession, "a tool call stamps the session, name and version")
	assert.Empty(t, sessionOf(generationAt(t, rows, 80673, 1010)), "the agent process itself carries no marker")
	assert.Equal(t, api.AgentSession{}, generationAt(t, rows, 80693, 1030).AgentSession, "a harness helper is in no session")

	assert.Equal(t, sessionOne, sessionOf(generationAt(t, rows, 80960, 2060)), "first seen at exec, inherited from its creator")
	assert.Equal(t, sessionOne, sessionOf(generationAt(t, rows, 80842, 3030)))
	assert.Equal(t, sessionOne, sessionOf(generationAt(t, rows, 80842, 3040)), "env -i's re-exec keeps the session")

	assert.Equal(t, sessionTwo, sessionOf(generationAt(t, rows, 80900, 4010)), "a new marker starts a new session")
	assert.Equal(t, sessionTwo, sessionOf(forkAt(t, rows, 80901, 4020)), "and the processes it creates carry it")

	assert.Empty(t, sessionOf(generationAt(t, rows, 80950, 5010)), "an oversized marker is ignored")
}

// spec:server-process-graph-builder/agent-session-stamping/a-daemonized-descendant-keeps-the-session
func TestAgentSession_DaemonizedDescendant(t *testing.T) {
	t.Parallel()

	t.Run("one batch", func(t *testing.T) {
		t.Parallel()
		rows := processInBatches(t, agentSessionScenario())
		daemon := generationAt(t, rows, 80732, 2050)
		require.True(t, daemon.Reparented)
		assert.Equal(t, sessionOne, sessionOf(daemon))
	})

	t.Run("one event per batch matches one batch", func(t *testing.T) {
		t.Parallel()
		bRef, bBatch, db := twoBuilders(t)
		requireBatchInvariant(t, db, bRef, bBatch, agentSessionScenario(), "agent-session-"+t.Name())
	})
}

// Property: for any fork forest where some execs carry a session marker, each process's session is the one on its nearest marked
// ancestor, itself included, or none. The forest is built the way the kernel numbers it, with each child naming its creator's
// current generation, so every creator link resolves.
func TestAgentSession_NearestMarkedAncestorProperty(t *testing.T) {
	t.Parallel()
	b, _, db := twoBuilders(t)
	iter := 0
	rapid.Check(t, func(rt *rapid.T) {
		iter++
		n := rapid.IntRange(1, 20).Draw(rt, "n")
		parentOf := make([]int, n)
		marker := make([]string, n)
		want := make([]string, n)
		var events []api.Event
		version := uint32(5000)
		current := map[int]uint32{}
		for i := range n {
			is := strconv.Itoa(i)
			pid := 2000 + i
			parentPID, parentVer := 1, uint32(1)
			if i > 0 {
				parentOf[i] = rapid.IntRange(0, i-1).Draw(rt, "parent"+is)
				parentPID, parentVer = 2000+parentOf[i], current[2000+parentOf[i]]
			}
			marker[i] = rapid.SampledFrom([]string{"", "", "s-one", "s-two"}).Draw(rt, "marker"+is)
			want[i] = marker[i]
			if want[i] == "" && i > 0 {
				want[i] = want[parentOf[i]]
			}
			ts := int64(10_000 + i*100)
			version++
			events = append(events, lineageEvt("f"+is, "fork", ts, `{"child_pid":`+strconv.Itoa(pid)+`,"parent_pid":`+strconv.Itoa(parentPID)+
				`,"pidversion":`+strconv.Itoa(int(version))+`,"parent_pidversion":`+strconv.Itoa(int(parentVer))+`}`))
			version++
			current[pid] = version
			env := ""
			if marker[i] != "" {
				env = "," + markers(marker[i])
			}
			events = append(events, lineageEvt("e"+is, "exec", ts+10, `{"pid":`+strconv.Itoa(pid)+`,"ppid":`+strconv.Itoa(parentPID)+
				`,"path":"/bin/p","pidversion":`+strconv.Itoa(int(version))+`,"parent_pidversion":`+strconv.Itoa(int(parentVer))+env+`}`))
		}
		host := "agent-pbt-" + strconv.Itoa(iter)
		require.NoError(rt, b.ProcessBatch(t.Context(), rewriteHost(events, host)))
		rows := dumpNormalizedForest(t, db, t.Context(), host)
		for i := range n {
			assert.Equal(rt, want[i], sessionOf(generationAt(t, rows, 2000+i, int64(10_000+i*100+10))), "process %d", i)
		}
	})
}
