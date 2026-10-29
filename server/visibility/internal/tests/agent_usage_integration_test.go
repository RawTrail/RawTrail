//go:build integration

package tests

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// usageEvent is one agent_usage event of a session's message; extra is spliced into the payload (a mode, an origin session).
func usageEvent(id, host, session, messageID, model string, in, out int64, atNs int64, extra string) visibilityapi.Event {
	return activityEvent(id, host, "agent_usage", atNs, `{"agent_session_id":"`+session+`","message_id":"`+messageID+`","model":"`+model+
		`","input_tokens":`+strconv.FormatInt(in, 10)+`,"output_tokens":`+strconv.FormatInt(out, 10)+
		`,"cache_creation_input_tokens":10,"cache_read_input_tokens":100,"thinking_tokens":1,"uid":501`+extra+`}`)
}

// spec:server-rest-api/agent-session-token-totals/a-session-s-tokens-are-totalled-by-model-and-its-messages-by-mode
// spec:server-rest-api/agent-session-token-totals/a-message-is-counted-once-and-a-copy-not-at-all
func TestEventArchive_AgentUsageTotals(t *testing.T) {
	t.Parallel()
	arch := openTestArchive(t)
	ctx := context.Background()
	base := time.Now().UnixNano()
	require.NoError(t, arch.Insert(ctx, []visibilityapi.Event{
		// The parent session's one message, which the fork below copies.
		usageEvent("u-parent", "h1", "parent", "msg_p", "claude-opus-5-5", 1, 1000, base, ``),
		usageEvent("u-1", "h1", "fork", "msg_1", "claude-opus-5-5", 3, 50, base+1, `,"permission_mode":"auto"`),
		// The agent restarted and sent msg_1 again: counted once, at its first recording.
		usageEvent("u-1-again", "h1", "fork", "msg_1", "claude-opus-5-5", 3, 50, base+9, `,"permission_mode":"auto"`),
		usageEvent("u-2", "h1", "fork", "msg_2", "claude-sonnet-5-5", 10, 20, base+2, `,"permission_mode":"bypassPermissions"`),
		usageEvent("u-3", "h1", "fork", "msg_3", "claude-opus-5-5", 4, 60, base+3, `,"permission_mode":"bypassPermissions"`),
		// The fork's copy of its parent's message names the parent, which holds it.
		usageEvent("u-copy", "h1", "fork", "msg_p", "claude-opus-5-5", 1, 1000, base+4, `,"origin_session_id":"parent"`),
		// After /clear a message names the first session, which does not hold it: the fork's own.
		usageEvent("u-clear", "h1", "fork", "msg_4", "claude-opus-5-5", 2, 5, base+5, `,"origin_session_id":"parent"`),
		usageEvent("u-other", "h1", "elsewhere", "msg_x", "claude-opus-5-5", 9, 9, base, ``),
	}))
	var got visibilityapi.AgentUsageTotals
	require.Eventually(t, func() bool {
		var err error
		got, err = arch.AgentUsageTotals(ctx, visibilityapi.AgentToolCallFilter{HostID: "h1", SessionID: "fork"})
		return err == nil && len(got.Models) == 2
	}, 5*time.Second, 100*time.Millisecond)
	assert.Equal(t, []visibilityapi.ModelUsage{
		{Model: "claude-opus-5-5", Messages: 3, Input: 9, Output: 115, CacheCreation: 30, CacheRead: 300, Thinking: 3},
		{Model: "claude-sonnet-5-5", Messages: 1, Input: 10, Output: 20, CacheCreation: 10, CacheRead: 100, Thinking: 1},
	}, got.Models, "heaviest output first; the copy is the parent's and the re-sent message counts once")
	assert.Equal(t, []visibilityapi.ModeUsage{
		{Mode: "auto", Messages: 1, FirstNs: base + 1, LastNs: base + 1},
		{Mode: "bypassPermissions", Messages: 2, FirstNs: base + 2, LastNs: base + 3},
		{Mode: "", Messages: 1, FirstNs: base + 5, LastNs: base + 5},
	}, got.Modes, "first seen first; a message written before any mode is named counts under none")

	empty, err := arch.AgentUsageTotals(ctx, visibilityapi.AgentToolCallFilter{HostID: "h1", SessionID: "none"})
	require.NoError(t, err)
	assert.Equal(t, visibilityapi.AgentUsageTotals{Models: []visibilityapi.ModelUsage{}, Modes: []visibilityapi.ModeUsage{}}, empty)
}

// However often the agent sends a message, and in whatever order its sends arrive, the totals are the sum over the session's
// distinct messages.
func TestEventArchive_AgentUsageTotalsAreTheSumOfTheMessages(t *testing.T) {
	t.Parallel()
	arch := openTestArchive(t)
	ctx := context.Background()
	base := time.Now().UnixNano()
	iter := 0
	rapid.Check(t, func(rt *rapid.T) {
		iter++
		session := "pbt-" + strconv.Itoa(iter)
		n := rapid.IntRange(1, 8).Draw(rt, "messages")
		var events []visibilityapi.Event
		var wantOut, wantIn int64
		for i := range n {
			in, out := rapid.Int64Range(0, 1000).Draw(rt, "in"), rapid.Int64Range(0, 1000).Draw(rt, "out")
			wantIn, wantOut = wantIn+in, wantOut+out
			msg := "msg_" + strconv.Itoa(i)
			for send := range rapid.IntRange(1, 3).Draw(rt, "sends") {
				events = append(events, usageEvent(session+"-"+msg+"-"+strconv.Itoa(send), "h1", session, msg, "claude-opus-5-5", in, out,
					base+int64(rapid.IntRange(0, 50).Draw(rt, "at")), ``))
			}
		}
		require.NoError(rt, arch.Insert(ctx, rapid.Permutation(events).Draw(rt, "order")))
		got, err := arch.AgentUsageTotals(ctx, visibilityapi.AgentToolCallFilter{HostID: "h1", SessionID: session})
		require.NoError(rt, err)
		require.Len(rt, got.Models, 1)
		var modeMessages int64
		for _, m := range got.Modes {
			modeMessages += m.Messages
		}
		require.Equal(rt, got.Models[0].Messages, modeMessages, "both breakdowns count the same messages")
		require.Equal(rt, visibilityapi.ModelUsage{Model: "claude-opus-5-5", Messages: int64(n), Input: wantIn, Output: wantOut,
			CacheCreation: 10 * int64(n), CacheRead: 100 * int64(n), Thinking: int64(n)}, got.Models[0])
	})
}
