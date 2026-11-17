package main

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/test/fakeagent"
)

// TestSessionCaptures_EachManifestHost checks every embedded session capture is what the demo promises: it belongs to the host it is
// woven onto, it carries each kind of agent event the Agent sessions views read, every held event's process is exec'd in the capture
// (or the materialization barrier would wait out its timeout), and the scrub left none of the capture machine's identity behind.
func TestSessionCaptures_EachManifestHost(t *testing.T) {
	t.Parallel()
	// The capture user, the VM's LAN, and anything shaped like a credential.
	leak := regexp.MustCompile(`(?i)victor|192\.168\.|edr-dev|BEGIN [A-Z ]*PRIVATE KEY|AKIA[0-9A-Z]{16}|sk-ant-|ghp_[A-Za-z0-9]{20}`)
	for _, host := range hostManifest {
		if host.Sessions == nil {
			continue
		}
		t.Run(host.Sessions.File, func(t *testing.T) {
			t.Parallel()
			_, hostID, err := loadHostEnvelopes(host.File)
			require.NoError(t, err)
			envs, sessionHost, err := loadSessionEnvelopes(host.Sessions.File)
			require.NoError(t, err)
			assert.Equal(t, hostID, sessionHost, "the capture carries the id of the host it is woven onto")

			types := map[string]int{}
			execs := map[int]bool{}
			for _, e := range envs {
				types[e.EventType]++
				if e.EventType == "exec" {
					pid, ok := envelopePID(e)
					require.True(t, ok)
					execs[pid] = true
				}
				assert.NotRegexp(t, leak, string(e.Payload), "%s %s", e.EventType, e.EventID)
			}
			for _, kind := range []string{"exec", "fork", "file_activity", "network_connect", "dns_query", "agent_prompt", "agent_tool_call",
				"agent_usage", "agent_credential_block"} {
				assert.Positive(t, types[kind], "capture carries %s events", kind)
			}
			for _, e := range envs {
				if !heldUntilProcess[e.EventType] {
					continue
				}
				pid, ok := envelopePID(e)
				require.True(t, ok)
				assert.True(t, execs[pid], "%s %s: pid %d is exec'd in the capture", e.EventType, e.EventID, pid)
			}

			// The session names its agent by the pid in the CLAUDE_PID marker, so the marker must name a claude the capture execs.
			claudes := map[string]bool{}
			for _, e := range envs {
				var p struct {
					PID  int      `json:"pid"`
					Args []string `json:"args"`
				}
				require.NoError(t, json.Unmarshal(e.Payload, &p))
				if e.EventType == "exec" && len(p.Args) > 0 && strings.HasSuffix(p.Args[0], "claude") {
					claudes[strconv.Itoa(p.PID)] = true
				}
			}
			markers := 0
			for _, e := range envs {
				var p struct {
					Env map[string]string `json:"env"`
				}
				require.NoError(t, json.Unmarshal(e.Payload, &p))
				if pid, ok := p.Env["CLAUDE_PID"]; ok {
					markers++
					assert.True(t, claudes[pid], "%s %s: CLAUDE_PID %s names a claude in the capture", e.EventType, e.EventID, pid)
				}
			}
			assert.Positive(t, markers, "the capture carries agent pid markers")

			ids, err := sessionIDs(envs)
			require.NoError(t, err)
			assert.Len(t, ids, 4, "an ordinary session, an honest credential read, a prompt injection, and a bypassPermissions session")
		})
	}
}

func payloadOf(t *testing.T, env fakeagent.Envelope) map[string]any {
	t.Helper()
	p, err := decodePayload(env.Payload)
	require.NoError(t, err)
	return p
}

func envelope(t *testing.T, eventType string, ts int64, payload map[string]any) fakeagent.Envelope {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return fakeagent.Envelope{EventID: eventType, EventType: eventType, TimestampNs: ts, Payload: raw}
}

func TestReparentSessionsToHost(t *testing.T) {
	t.Parallel()
	const anchor, anchorVersion = 811, int64(10042)
	envs := []fakeagent.Envelope{
		// claude is forked by a shell outside the capture, so the fork and the exec name an outside parent.
		envelope(t, "fork", 1, map[string]any{"child_pid": 23953, "parent_pid": 23951, "pidversion": 7496, "parent_pidversion": 7491}),
		envelope(t, "exec", 2, map[string]any{"pid": 23953, "ppid": 23951, "original_ppid": 23951, "parent_pidversion": 7491,
			"responsible_pid": 23947, "responsible_pidversion": 7486, "pidversion": 7497}),
		// A tool call's shell names claude, which the capture holds, and launchd as its responsible process.
		envelope(t, "exec", 3, map[string]any{"pid": 23976, "ppid": 23953, "parent_pidversion": 7497, "responsible_pid": 1,
			"pidversion": 7530}),
		envelope(t, "file_activity", 4, map[string]any{"pid": 23976, "pidversion": 7530, "path": "/Users/alex/src/a.js"}),
	}
	require.NoError(t, reparentSessionsToHost(envs, anchor, anchorVersion))

	fork := payloadOf(t, envs[0])
	assert.Equal(t, json.Number("811"), fork["parent_pid"], "the outside shell becomes the host's shell")
	assert.Equal(t, json.Number("10042"), fork["parent_pidversion"], "with that shell's generation")
	assert.Equal(t, json.Number("23953"), fork["child_pid"])

	claude := payloadOf(t, envs[1])
	for field, want := range map[string]string{"ppid": "811", "original_ppid": "811", "parent_pidversion": "10042",
		"responsible_pid": "811", "responsible_pidversion": "10042", "pidversion": "7497"} {
		assert.Equal(t, json.Number(want), claude[field], field)
	}

	shell := payloadOf(t, envs[2])
	assert.Equal(t, json.Number("23953"), shell["ppid"], "a parent inside the capture is kept")
	assert.Equal(t, json.Number("7497"), shell["parent_pidversion"])
	assert.Equal(t, json.Number("1"), shell["responsible_pid"], "launchd is kept")

	assert.JSONEq(t, `{"pid":23976,"pidversion":7530,"path":"/Users/alex/src/a.js"}`, string(envs[3].Payload),
		"an event naming no parent is left byte for byte")
}

func TestShiftSessionEnvelopes(t *testing.T) {
	t.Parallel()
	const captured = int64(1_794_925_130_000_000_000)
	envs := []fakeagent.Envelope{
		envelope(t, "file_activity", captured+5_000, map[string]any{"pid": 2, "last_timestamp_ns": captured + 7_000}),
		envelope(t, "exec", captured, map[string]any{"pid": 2, "start_time_ns": captured - 1_000}),
		envelope(t, "agent_prompt", captured+2_000, map[string]any{"agent_session_id": "s", "text": "hi"}),
	}
	end := time.Unix(0, 1_900_000_000_000_000_000)
	require.NoError(t, shiftSessionEnvelopes(envs, end))

	delta := end.UnixNano() - (captured + 5_000)
	assert.Equal(t, []string{"exec", "agent_prompt", "file_activity"}, []string{envs[0].EventType, envs[1].EventType, envs[2].EventType},
		"sorted by time")
	assert.Equal(t, end.UnixNano(), envs[2].TimestampNs, "the latest event lands at end")
	assert.Equal(t, int64(2_000), envs[1].TimestampNs-envs[0].TimestampNs, "gaps are kept")
	assert.Equal(t, json.Number("1900000000000002000"), payloadOf(t, envs[2])["last_timestamp_ns"],
		"a file activity fold keeps its 2 µs after its event")
	assert.Equal(t, captured-1_000+delta, mustInt(t, payloadOf(t, envs[0])["start_time_ns"]), "a process start moves with its exec")
	assert.JSONEq(t, `{"agent_session_id":"s","text":"hi"}`, string(envs[1].Payload), "a payload without device times is untouched")
}

func mustInt(t *testing.T, v any) int64 {
	t.Helper()
	n, ok := v.(json.Number)
	require.True(t, ok)
	i, err := n.Int64()
	require.NoError(t, err)
	return i
}

func TestSessionIDs(t *testing.T) {
	t.Parallel()
	envs := []fakeagent.Envelope{
		envelope(t, "agent_prompt", 1, map[string]any{"agent_session_id": "a"}),
		envelope(t, "agent_prompt", 2, map[string]any{"agent_session_id": "a"}),
		envelope(t, "agent_tool_call", 3, map[string]any{"agent_session_id": "b"}),
		envelope(t, "agent_prompt", 4, map[string]any{"agent_session_id": "c"}),
	}
	ids, err := sessionIDs(envs)
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"a": true, "c": true}, ids, "sessions are counted from their prompts")
}
