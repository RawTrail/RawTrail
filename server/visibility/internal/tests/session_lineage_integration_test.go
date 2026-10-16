//go:build integration

package tests

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	visibilityapi "github.com/fleetdm/edr/server/visibility/api"
	"github.com/fleetdm/edr/server/visibility/testkit"
)

// Sessions forked from each other (issue #1347), as Claude Code 2.1.300 writes them on edr-dev: a /fork begins with a copy of its
// parent's history, each copied line keeping the session id it was written under, and the fork's own calls name no other session. A
// copied call is its origin's, not the fork's, and the latest copy names the parent. Run against ClickHouse and the in-memory archive,
// so the fake cannot drift from the store.
//
// spec:server-rest-api/agent-session-lineage-endpoint/a-session-names-its-parent-and-its-forks
// spec:server-rest-api/agent-session-lineage-endpoint/another-user-s-transcripts-do-not-make-a-lineage
// spec:server-rest-api/agent-session-lineage-endpoint/a-fork-without-its-own-evidence-is-read-as-the-earlier-session-s
// spec:server-rest-api/copied-agent-tool-calls/a-copied-call-is-listed-under-its-origin-only
func TestEventArchive_AgentSessionLineage(t *testing.T) {
	t.Parallel()
	base := time.Now().UnixNano()
	at := func(s int64) int64 { return base + s*int64(time.Second) }
	call := func(id, host, session, toolUseID, origin string, uid int, ts int64) visibilityapi.Event {
		extra := ""
		if origin != "" {
			extra = `,"origin_session_id":"` + origin + `"`
		}
		return activityEvent(id, host, "agent_tool_call", ts, `{"agent_session_id":"`+session+`","tool_use_id":"`+toolUseID+`",`+
			`"tool_name":"Bash","command":"echo `+toolUseID+`","uid":`+strconv.Itoa(uid)+extra+`}`)
	}
	events := []visibilityapi.Event{
		// The parent's own calls.
		call("p1", "h1", "parent", "toolu_a", "", 501, at(1)),
		call("p2", "h1", "parent", "toolu_b", "", 501, at(2)),
		// The fork copied toolu_a, keeping its time, then made toolu_c.
		call("f1", "h1", "fork", "toolu_a", "parent", 501, at(1)),
		call("f2", "h1", "fork", "toolu_c", "", 501, at(10)),
		// A fork of the fork copied both, each naming the session it was written under, then made toolu_d.
		call("g1", "h1", "grandfork", "toolu_a", "parent", 501, at(1)),
		call("g2", "h1", "grandfork", "toolu_c", "fork", 501, at(10)),
		call("g3", "h1", "grandfork", "toolu_d", "", 501, at(20)),
		// After /clear, the parent's process writes its first session's id on the new session's own call: not a copy, since the
		// parent never held toolu_e.
		call("k1", "h1", "cleared", "toolu_e", "parent", 501, at(15)),
		// Another user's line naming the parent and its call: the parent holds toolu_b under 501 only, so it is no copy of it.
		call("x1", "h1", "forged", "toolu_b", "parent", 502, at(16)),
		// Another user wrote two transcripts in their own home: one named for a decoy parent, and one named for the real parent holding
		// a later copy of the decoy's call. Both are 502's, so they say nothing about 501's session.
		call("y1", "h1", "decoy", "toolu_y", "", 502, at(30)),
		call("y2", "h1", "parent", "toolu_y", "decoy", 502, at(31)),
		// Known limits, which only direct fork evidence could close. A fork made after /clear copies the cleared session's call naming
		// the process's first session, which never held it: the copy reads as the fork's own, and gives no link.
		call("c1", "h1", "fork-of-cleared", "toolu_e", "parent", 501, at(15)),
		call("c2", "h1", "fork-of-cleared", "toolu_f", "", 501, at(40)),
		// A fork of a fork that made no calls of its own holds copies naming the session before it only, so it reads as that session's.
		call("q1", "h1", "quiet-fork", "toolu_b", "parent", 501, at(2)),
		call("q2", "h1", "fork-of-quiet-fork", "toolu_b", "parent", 501, at(2)),
		call("q3", "h1", "fork-of-quiet-fork", "toolu_g", "", 501, at(50)),
		// Another host's copy is that host's.
		call("o1", "h2", "elsewhere", "toolu_a", "parent", 501, at(1)),
		call("o2", "h2", "parent", "toolu_a", "", 501, at(1)),
	}
	for name, arch := range map[string]visibilityapi.EventArchive{"clickhouse": openTestArchive(t), "memory": testkit.NewMemArchive()} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			require.NoError(t, arch.Insert(ctx, events))
			calls := func(session string) []string {
				got, _, err := arch.AgentToolCalls(ctx, visibilityapi.AgentToolCallFilter{HostID: "h1", SessionID: session, Limit: 10})
				require.NoError(t, err)
				ids := []string{}
				for _, c := range got {
					ids = append(ids, c.ToolUseID)
				}
				return ids
			}
			require.Eventually(t, func() bool { return len(calls("grandfork")) == 1 }, 5*time.Second, 100*time.Millisecond)
			assert.Equal(t, []string{"toolu_a", "toolu_b"}, calls("parent"))
			assert.Equal(t, []string{"toolu_c"}, calls("fork"), "the copy of the parent's call is not the fork's")
			assert.Equal(t, []string{"toolu_d"}, calls("grandfork"))
			assert.Equal(t, []string{"toolu_e"}, calls("cleared"), "a call the named session never held is the session's own")
			assert.Equal(t, []string{"toolu_b"}, calls("forged"))
			assert.Equal(t, []string{"toolu_e", "toolu_f"}, calls("fork-of-cleared"), "known limit: the post-/clear copy reads as its own")

			lineage := func(session string) visibilityapi.AgentSessionLineage {
				got, err := arch.AgentSessionLineage(ctx, "h1", session, 501)
				require.NoError(t, err)
				return got
			}
			assert.Equal(t, visibilityapi.AgentSessionLineage{ForkedSessionIDs: []string{"fork", "fork-of-quiet-fork", "quiet-fork"}},
				lineage("parent"), "the fork of the fork is its parent's, and neither /clear nor another user's transcripts make a fork; "+
					"known limit: the fork of the quiet fork reads as the parent's")
			assert.Equal(t, visibilityapi.AgentSessionLineage{ForkedFromSessionID: "parent", ForkedSessionIDs: []string{"grandfork"}},
				lineage("fork"))
			assert.Equal(t, visibilityapi.AgentSessionLineage{ForkedFromSessionID: "fork", ForkedSessionIDs: []string{}}, lineage("grandfork"),
				"the latest copy names the parent, not the first session in the chain")
			assert.Equal(t, visibilityapi.AgentSessionLineage{ForkedSessionIDs: []string{}}, lineage("cleared"))
			assert.Equal(t, visibilityapi.AgentSessionLineage{ForkedSessionIDs: []string{}}, lineage("fork-of-cleared"))
		})
	}
}
