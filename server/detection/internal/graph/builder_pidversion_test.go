package graph

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/fleetdm/edr/server/detection/api"
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

// TestExitPayload_PIDVersionRoundTrip pins the exiting process's generation on the server's exit payload (issue #1279): any value
// round-trips exactly under the pidversion key, and an absent one stays absent.
//
// spec:endpoint-event-collection/exit-and-file-event-process-generation/an-exit-names-the-generation-that-exited
func TestExitPayload_PIDVersionRoundTrip(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		in := exitPayload{
			PID:      rapid.IntRange(1, 1<<20).Draw(rt, "pid"),
			ExitCode: rapid.IntRange(-1, 255).Draw(rt, "exit_code"),
		}
		if rapid.Bool().Draw(rt, "has_pidversion") {
			in.PIDVersion = new(rapid.Uint32().Draw(rt, "pidversion"))
		}
		b, err := json.Marshal(in)
		require.NoError(rt, err)
		if in.PIDVersion == nil {
			assert.NotContains(rt, string(b), `"pidversion"`)
		} else {
			assert.Contains(rt, string(b), `"pidversion":`)
		}
		var out exitPayload
		require.NoError(rt, json.Unmarshal(b, &out))
		assert.Equal(rt, in, out)
	})
}

// TestExecForkPayload_LineageRoundTrip pins the lineage fields the extension sends (issue #1277) on the server's exec and fork
// payloads (issue #1280): any combination round-trips exactly under the wire keys, and an absent field stays absent.
//
// spec:server-process-graph-builder/process-lineage-persistence/a-legacy-event-leaves-the-lineage-unset
func TestExecForkPayload_LineageRoundTrip(t *testing.T) {
	t.Parallel()
	optUint32 := func(rt *rapid.T, label string) *uint32 {
		if rapid.Bool().Draw(rt, "has_"+label) {
			return new(rapid.Uint32().Draw(rt, label))
		}
		return nil
	}
	optInt := func(rt *rapid.T, label string) *int {
		if rapid.Bool().Draw(rt, "has_"+label) {
			return new(rapid.IntRange(0, 1<<20).Draw(rt, label))
		}
		return nil
	}
	optString := func(rt *rapid.T, label string) *string {
		if rapid.Bool().Draw(rt, "has_"+label) {
			return new(rapid.StringMatching(`/[a-z0-9 /]{1,24}`).Draw(rt, label))
		}
		return nil
	}

	t.Run("exec", func(t *testing.T) {
		t.Parallel()
		rapid.Check(t, func(rt *rapid.T) {
			in := execPayload{PID: rapid.IntRange(1, 1<<20).Draw(rt, "pid"), PPID: rapid.IntRange(0, 1<<20).Draw(rt, "ppid"), Path: "/bin/sh"}
			in.ParentPIDVersion = optUint32(rt, "parent_pidversion")
			in.OriginalPPID = optInt(rt, "original_ppid")
			in.ResponsiblePID = optInt(rt, "responsible_pid")
			in.ResponsiblePIDVersion = optUint32(rt, "responsible_pidversion")
			in.SessionID = optInt(rt, "session_id")
			in.GroupID = optInt(rt, "group_id")
			in.TTY = optString(rt, "tty")
			if rapid.Bool().Draw(rt, "has_start_time_ns") {
				in.StartTimeNs = new(rapid.Int64Min(0).Draw(rt, "start_time_ns"))
			}
			in.CWD = optString(rt, "cwd")

			b, err := json.Marshal(in)
			require.NoError(rt, err)
			if in.OriginalPPID != nil {
				assert.Contains(rt, string(b), `"original_ppid":`)
			}
			var out execPayload
			require.NoError(rt, json.Unmarshal(b, &out))
			assert.Equal(rt, in, out)
		})
	})

	t.Run("fork", func(t *testing.T) {
		t.Parallel()
		rapid.Check(t, func(rt *rapid.T) {
			in := forkPayload{ChildPID: rapid.IntRange(1, 1<<20).Draw(rt, "child"), ParentPID: rapid.IntRange(1, 1<<20).Draw(rt, "parent")}
			in.PIDVersion = optUint32(rt, "pidversion")
			in.ParentPIDVersion = optUint32(rt, "parent_pidversion")
			b, err := json.Marshal(in)
			require.NoError(rt, err)
			var out forkPayload
			require.NoError(rt, json.Unmarshal(b, &out))
			assert.Equal(rt, in, out)
		})
	})

	// The exact wire shape the extension sends, so a renamed tag cannot round-trip silently.
	t.Run("extension wire keys", func(t *testing.T) {
		t.Parallel()
		var exec execPayload
		require.NoError(t, json.Unmarshal([]byte(`{"pid":2316,"ppid":1,"path":"/bin/sleep","args":[],"cwd":"/private/tmp","uid":501,`+
			`"gid":20,"pidversion":5445,"original_ppid":2315,"parent_pidversion":7,"responsible_pid":2288,"responsible_pidversion":5391,`+
			`"session_id":2316,"group_id":2316,"tty":"/dev/ttys004","start_time_ns":1791030436491552000}`), &exec))
		assert.Equal(t, execPayload{
			PID: 2316, PPID: 1, Path: "/bin/sleep", Args: []byte(`[]`), UID: new(501), GID: new(20), PIDVersion: new(uint32(5445)),
			ParentPIDVersion: new(uint32(7)),
			ExecLineage: api.ExecLineage{
				OriginalPPID: new(2315), ResponsiblePID: new(2288), ResponsiblePIDVersion: new(uint32(5391)), SessionID: new(2316),
				GroupID: new(2316), TTY: new("/dev/ttys004"), StartTimeNs: new(int64(1791030436491552000)), CWD: new("/private/tmp"),
			},
		}, exec)

		var fork forkPayload
		require.NoError(t, json.Unmarshal([]byte(`{"child_pid":2316,"parent_pid":2315,"pidversion":5444,"parent_pidversion":5443}`), &fork))
		assert.Equal(t, forkPayload{ChildPID: 2316, ParentPID: 2315, PIDVersion: new(uint32(5444)), ParentPIDVersion: new(uint32(5443))}, fork)
	})
}

// TestExecPayload_Lineage pins how an exec's reported lineage becomes the stored one (issue #1280): reparented is derived from the
// current parent and the creator, never taken from the wire, and an empty cwd from an agent that predates the field is unknown.
//
// spec:server-process-graph-builder/process-lineage-persistence/a-daemonized-process-is-marked-reparented
func TestExecPayload_Lineage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		payload        string
		wantReparented bool
		wantCWD        *string
	}{
		{
			name: "daemon adopted by launchd", payload: `{"pid":9,"ppid":1,"original_ppid":3186,"cwd":"/tmp"}`,
			wantReparented: true, wantCWD: new("/tmp"),
		},
		{name: "ordinary child", payload: `{"pid":9,"ppid":3186,"original_ppid":3186,"cwd":"/tmp"}`, wantCWD: new("/tmp")},
		{name: "launched by launchd itself", payload: `{"pid":9,"ppid":1,"original_ppid":1}`},
		{name: "older agent without original_ppid", payload: `{"pid":9,"ppid":1,"cwd":""}`},
		{name: "wire claim of reparented is ignored", payload: `{"pid":9,"ppid":3186,"original_ppid":3186,"reparented":true}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var p execPayload
			require.NoError(t, json.Unmarshal([]byte(tc.payload), &p))
			got := p.lineage()
			assert.Equal(t, tc.wantReparented, got.Reparented)
			assert.Equal(t, tc.wantCWD, got.CWD)
		})
	}
}
