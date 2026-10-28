//go:build !windows

package agenttranscript

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spec:endpoint-event-collection/agent-token-usage-reporting/a-streamed-message-is-counted-once-with-its-turn-and-mode
func TestReader_SendsEachMessagesUsageOnce(t *testing.T) {
	t.Parallel()
	for _, capture := range []bool{true, false} {
		q := &recordingQueue{}
		r, path := newTestReader(t, q, "H1")
		r.prompts = capture
		mode := `{"type":"permission-mode","permissionMode":"bypassPermissions","sessionId":"47abb2a1-d231-4304-8cb1-52f4460fcd11"}`
		foreignMode := `{"type":"permission-mode","permissionMode":"plan","sessionId":"99999999-0000-4000-8000-000000000000"}`
		// Two entries of one streamed message, then a second message.
		appendTo(t, path, userLine(`"fix it"`, typed)+"\n"+mode+"\n"+foreignMode+"\n"+usageLine("msg_01", "claude-opus-5-5", "")+"\n"+
			usageLine("msg_01", "claude-opus-5-5", "")+"\n"+usageLine("msg_02", "claude-sonnet-5-5", "")+"\n")
		// The streamed message's second entry carries its own, later time, as Claude Code writes them.
		appendTo(t, path, strings.Replace(usageLine("msg_01", "claude-opus-5-5", ""), "09:31:00.500Z", "09:31:02.000Z", 1)+"\n")
		stats := r.RunOnce(context.Background())
		assert.Equal(t, 2, stats.Usage, "a streamed message is sent once, from its first entry")
		byID := map[string]map[string]any{}
		for _, e := range q.events {
			if e["event_type"] == UsageEventType {
				payload, _ := e["payload"].(map[string]any)
				byID[e["event_id"].(string)] = payload
			}
		}
		require.Len(t, byID, 2, "capture=%v: two messages, two event ids", capture)
		for _, e := range q.events {
			if payload, _ := e["payload"].(map[string]any); e["event_type"] == UsageEventType && payload["message_id"] == "msg_01" {
				assert.Equal(t, time.Date(2026, 10, 28, 9, 31, 0, 500_000_000, time.UTC).UnixNano(), int64(e["timestamp_ns"].(float64)),
					"the first entry's time, so a re-read sends the same row")
			}
		}
		for _, payload := range byID {
			assert.Equal(t, "p-1", payload["prompt_id"], "the turn of the prompt before it")
			assert.Equal(t, "bypassPermissions", payload["permission_mode"], "the mode its own session put in force")
			assert.InDelta(t, float64(os.Getuid()), payload["uid"], 0, "stamped with the transcript owner's uid")
		}
	}
}

// A restarted agent reads a recent transcript from its start again. Each message it sends again is the same row: the same event id at
// the same time, which the archive keeps once.
func TestReader_ARereadSendsTheSameUsageRows(t *testing.T) {
	t.Parallel()
	q := &recordingQueue{}
	r, path := newTestReader(t, q, "H1")
	appendTo(t, path, usageLine("msg_01", "claude-opus-5-5", "")+"\n"+
		strings.Replace(usageLine("msg_01", "claude-opus-5-5", ""), "09:31:00.500Z", "09:31:02.000Z", 1)+"\n")
	r.RunOnce(context.Background())
	restarted := New(r.root, r.hostID, q.enqueue, r.logger, true)
	restarted.RunOnce(context.Background())
	var rows []string
	for _, e := range q.events {
		if e["event_type"] == UsageEventType {
			rows = append(rows, e["event_id"].(string)+"@"+strconv.FormatFloat(e["timestamp_ns"].(float64), 'f', 0, 64))
		}
	}
	require.Len(t, rows, 2, "once per read of the transcript")
	assert.Equal(t, rows[0], rows[1])
}
