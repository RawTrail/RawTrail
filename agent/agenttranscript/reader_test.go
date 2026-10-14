//go:build !windows

package agenttranscript

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingQueue struct {
	events []map[string]any
	fail   bool
}

func (q *recordingQueue) enqueue(_ context.Context, body []byte) error {
	if q.fail {
		return errors.New("queue full")
	}
	var env map[string]any
	if err := json.Unmarshal(body, &env); err != nil {
		return err
	}
	q.events = append(q.events, env)
	return nil
}

func (q *recordingQueue) toolUseIDs() []string {
	var ids []string
	for _, e := range q.events {
		payload, _ := e["payload"].(map[string]any)
		id, _ := payload["tool_use_id"].(string)
		ids = append(ids, id)
	}
	return ids
}

func bashLine(id, command string) string {
	return assistantLine(`[{"type":"tool_use","id":"`+id+`","name":"Bash","input":{"command":"`+command+`"}}]`) + "\n"
}

// newTestReader returns a Reader over a temporary root holding one user's transcript, and the transcript's path.
func newTestReader(t *testing.T, q *recordingQueue, hostID string) (*Reader, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "dev", ".claude", "projects", "-Users-dev-project")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	path := filepath.Join(dir, "47abb2a1-d231-4304-8cb1-52f4460fcd11.jsonl")
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	r := New(root, func() string { return hostID }, q.enqueue, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return r, path
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- the test's own temporary transcript
	require.NoError(t, err)
	_, err = f.WriteString(text)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

// spec:endpoint-event-collection/agent-tool-call-capture/a-bash-tool-call-is-reported-with-its-command
func TestReader_TailsTranscripts(t *testing.T) {
	t.Parallel()
	q := &recordingQueue{}
	r, path := newTestReader(t, q, "H1")
	ctx := context.Background()

	appendTo(t, path, bashLine("toolu_01", "ls")+`{"type":"user","message":{"content":"hi"}}`+"\n"+`{"type":"assis`)
	assert.Equal(t, PassStats{Calls: 1}, r.RunOnce(ctx), "the half-written line waits")
	require.Len(t, q.events, 1)
	assert.Equal(t, "H1", q.events[0]["host_id"])
	assert.Equal(t, EventType, q.events[0]["event_type"])
	assert.InDelta(t, float64(time.Date(2026, 10, 14, 8, 50, 0, 512_000_000, time.UTC).UnixNano()), q.events[0]["timestamp_ns"], 1e3,
		"stamped at the transcript's time")

	// The rest of the half-written line arrives with another call; nothing is read twice.
	appendTo(t, path, `tant","message":{"content":"ok"}}`+"\n"+bashLine("toolu_02", "pwd"))
	assert.Equal(t, PassStats{Calls: 1}, r.RunOnce(ctx))
	assert.Equal(t, []string{"toolu_01", "toolu_02"}, q.toolUseIDs())
	assert.Equal(t, PassStats{}, r.RunOnce(ctx), "an unchanged transcript reads nothing")
}

// spec:endpoint-event-collection/agent-tool-call-capture/an-unreadable-line-is-skipped-not-guessed
func TestReader_SkipsUnreadableLines(t *testing.T) {
	t.Parallel()
	q := &recordingQueue{}
	r, path := newTestReader(t, q, "H1")
	appendTo(t, path, "not json\n"+bashLine("toolu_01", "ls"))
	assert.Equal(t, PassStats{Calls: 1, Unreadable: 1}, r.RunOnce(context.Background()))
}

func TestReader_RetriesWhatTheQueueRefused(t *testing.T) {
	t.Parallel()
	q := &recordingQueue{fail: true}
	r, path := newTestReader(t, q, "H1")
	appendTo(t, path, bashLine("toolu_01", "ls"))
	r.RunOnce(context.Background())
	q.fail = false
	r.RunOnce(context.Background())
	assert.Equal(t, []string{"toolu_01"}, q.toolUseIDs())
}

func TestReader_ReplacedTranscriptIsReadFromItsStart(t *testing.T) {
	t.Parallel()
	q := &recordingQueue{}
	r, path := newTestReader(t, q, "H1")
	appendTo(t, path, bashLine("toolu_01", "ls")+bashLine("toolu_02", "pwd"))
	r.RunOnce(context.Background())
	require.NoError(t, os.WriteFile(path, []byte(bashLine("toolu_03", "id")), 0o600))
	r.RunOnce(context.Background())
	assert.Equal(t, []string{"toolu_01", "toolu_02", "toolu_03"}, q.toolUseIDs())
}

// Users own the directories the agent reads as root: a planted symlink is not followed, and a FIFO cannot block the read.
func TestReader_ReadsOnlyRegularFiles(t *testing.T) {
	t.Parallel()
	q := &recordingQueue{}
	r, path := newTestReader(t, q, "H1")
	dir := filepath.Dir(path)
	secret := filepath.Join(t.TempDir(), "root-only.jsonl")
	require.NoError(t, os.WriteFile(secret, []byte(bashLine("toolu_secret", "cat /etc/master.passwd")), 0o600))
	require.NoError(t, os.Symlink(secret, filepath.Join(dir, "planted.jsonl")))
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "fifo.jsonl"), 0o600))
	appendTo(t, path, bashLine("toolu_01", "ls"))

	done := make(chan PassStats, 1)
	go func() { done <- r.RunOnce(context.Background()) }()
	select {
	case stats := <-done:
		assert.Equal(t, PassStats{Calls: 1}, stats)
	case <-time.After(5 * time.Second):
		t.Fatal("a FIFO blocked the read")
	}
	assert.Equal(t, []string{"toolu_01"}, q.toolUseIDs(), "the symlinked file was not read")
}

func TestReader_SkipsWhatItCannotAttribute(t *testing.T) {
	t.Parallel()
	t.Run("no host id yet", func(t *testing.T) {
		t.Parallel()
		q := &recordingQueue{}
		r, path := newTestReader(t, q, "")
		appendTo(t, path, bashLine("toolu_01", "ls"))
		assert.Equal(t, PassStats{}, r.RunOnce(context.Background()))
		assert.Empty(t, q.events)
	})
	t.Run("a finished session's transcript", func(t *testing.T) {
		t.Parallel()
		q := &recordingQueue{}
		r, path := newTestReader(t, q, "H1")
		appendTo(t, path, bashLine("toolu_01", "ls"))
		r.RunOnce(context.Background())
		require.Len(t, r.offsets, 1)
		appendTo(t, path, bashLine("toolu_02", "pwd"))
		old := time.Now().Add(-2 * recentWindow)
		require.NoError(t, os.Chtimes(path, old, old))
		assert.Equal(t, PassStats{}, r.RunOnce(context.Background()), "a transcript gone quiet is not read")
		assert.Empty(t, r.offsets, "and its offset is forgotten")
	})
}
