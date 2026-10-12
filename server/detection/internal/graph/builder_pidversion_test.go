package graph

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// TestExecForkPayload_PIDVersionRoundTrip is the wire-field round-trip for the optional pidversion added to the exec and fork
// payloads (issue #403). It pins that the field survives Marshal/Unmarshal across present and absent values and that an absent
// JSON key decodes to nil (correlation falls back to the window) rather than 0.
func TestExecForkPayload_PIDVersionRoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("exec absent key decodes to nil", func(t *testing.T) {
		t.Parallel()
		var got execPayload
		require.NoError(t, json.Unmarshal([]byte(`{"pid":1,"ppid":2,"path":"/bin/ls","args":[],"cwd":"/","uid":0,"gid":0}`), &got))
		assert.Nil(t, got.PIDVersion)
	})

	t.Run("fork present value round-trips", func(t *testing.T) {
		t.Parallel()
		in := forkPayload{ChildPID: 10, ParentPID: 1, PIDVersion: new(uint32(99))}
		b, err := json.Marshal(in)
		require.NoError(t, err)
		var out forkPayload
		require.NoError(t, json.Unmarshal(b, &out))
		assert.Equal(t, in, out)
	})

	// The lineage fields (issue #1277) reach the server before the builder stores them (#1280). Until then they must decode to
	// exactly what the same payload without them decodes to, so an agent that sends them builds the same process rows.
	//
	// spec:endpoint-event-collection/process-lineage-identity-capture/an-event-without-lineage-keeps-its-shape
	t.Run("lineage fields leave the decoded payloads unchanged", func(t *testing.T) {
		t.Parallel()
		const lineage = `"original_ppid":2315,"parent_pidversion":7,"responsible_pid":2288,"responsible_pidversion":5391,` +
			`"session_id":2316,"group_id":2316,"tty":"/dev/ttys004","start_time_ns":1791030436491552000`
		const execBase = `"pid":2316,"ppid":1,"path":"/bin/sleep","args":["/bin/sleep"],"cwd":"/private/tmp","uid":501,"gid":20,"pidversion":5445`

		var plainExec, lineageExec execPayload
		require.NoError(t, json.Unmarshal([]byte(`{`+execBase+`}`), &plainExec))
		require.NoError(t, json.Unmarshal([]byte(`{`+execBase+`,`+lineage+`}`), &lineageExec))
		assert.Equal(t, plainExec, lineageExec)

		var plainFork, lineageFork forkPayload
		require.NoError(t, json.Unmarshal([]byte(`{"child_pid":2316,"parent_pid":2315,"pidversion":5444}`), &plainFork))
		require.NoError(t, json.Unmarshal([]byte(`{"child_pid":2316,"parent_pid":2315,"pidversion":5444,"parent_pidversion":5443}`), &lineageFork))
		assert.Equal(t, plainFork, lineageFork)
	})

	rapid.Check(t, func(rt *rapid.T) {
		in := execPayload{
			PID:  rapid.IntRange(1, 1<<20).Draw(rt, "pid"),
			PPID: rapid.IntRange(0, 1<<20).Draw(rt, "ppid"),
			Path: rapid.StringMatching(`/[a-z/]{1,12}`).Draw(rt, "path"),
		}
		if rapid.Bool().Draw(rt, "has_pidversion") {
			in.PIDVersion = new(rapid.Uint32().Draw(rt, "pidversion"))
		}
		b, err := json.Marshal(in)
		require.NoError(rt, err)
		var out execPayload
		require.NoError(rt, json.Unmarshal(b, &out))
		assert.Equal(rt, in.PIDVersion, out.PIDVersion)
	})
}

// TestExecPayload_AgentSessionEnvRoundTrip pins the agent session markers (issue #1278) on the server's exec payload: any marker
// map round-trips exactly, and an absent one stays absent rather than becoming an empty object.
//
// spec:endpoint-event-collection/agent-session-markers-capture/a-process-without-markers-keeps-its-shape
func TestExecPayload_AgentSessionEnvRoundTrip(t *testing.T) {
	t.Parallel()
	names := []string{
		"CLAUDE_CODE_SESSION_ID", "CLAUDE_PID", "CLAUDECODE", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_ENTRYPOINT",
		"CLAUDE_CODE_SESSION_ATTENDED", "AI_AGENT",
	}
	rapid.Check(t, func(rt *rapid.T) {
		in := execPayload{PID: rapid.IntRange(1, 1<<20).Draw(rt, "pid"), Path: "/bin/zsh"}
		if rapid.Bool().Draw(rt, "has_env") {
			in.Env = rapid.MapOfN(rapid.SampledFrom(names), rapid.StringN(0, 256, -1), 1, len(names)).Draw(rt, "env")
		}
		b, err := json.Marshal(in)
		require.NoError(rt, err)
		if in.Env == nil {
			assert.NotContains(rt, string(b), `"env"`)
		} else {
			assert.Contains(rt, string(b), `"env":{`)
		}
		var out execPayload
		require.NoError(rt, json.Unmarshal(b, &out))
		assert.Equal(rt, in, out)
	})
}
