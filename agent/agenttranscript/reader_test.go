//go:build !windows

package agenttranscript

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingQueue struct {
	events []map[string]any
	fail   bool
}

// capturing is the prompt switch of a reader whose prompts are captured.
func capturing() bool { return true }

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

// bashLineIn is a Bash call of the given session, for a transcript named for that session.
func bashLineIn(session, id, command string) string {
	return strings.Replace(bashLine(id, command), "47abb2a1-d231-4304-8cb1-52f4460fcd11", session, 1)
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
	r := New(root, func() string { return hostID }, q.enqueue, slog.New(slog.NewTextHandler(io.Discard, nil)), capturing)
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

// spec:endpoint-event-collection/agent-tool-call-reporting/a-bash-tool-call-is-reported-with-its-command
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

// spec:endpoint-event-collection/agent-tool-call-reporting/an-unreadable-line-is-skipped-not-guessed
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

// The home is the user's to arrange: a .claude/projects or project directory linked outside it is not followed out of it.
func TestReader_StaysInsideTheHome(t *testing.T) {
	t.Parallel()
	outside := t.TempDir()
	project := filepath.Join(outside, "-Users-dev-project")
	require.NoError(t, os.MkdirAll(project, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(project, "s.jsonl"), []byte(bashLine("toolu_outside", "id")), 0o600))
	for name, link := range map[string]string{
		"a linked projects directory": filepath.Join(".claude", "projects"),
		"a linked project directory":  filepath.Join(".claude", "projects", "-Users-dev-project"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			at := filepath.Join(root, "dev", link)
			require.NoError(t, os.MkdirAll(filepath.Dir(at), 0o700))
			target := outside
			if filepath.Base(link) != "projects" {
				target = project
			}
			require.NoError(t, os.Symlink(target, at))
			q := &recordingQueue{}
			r := New(root, func() string { return "H1" }, q.enqueue, slog.New(slog.NewTextHandler(io.Discard, nil)), capturing)
			assert.Equal(t, PassStats{}, r.RunOnce(context.Background()))
			assert.Empty(t, q.events)
		})
	}
}

// A line longer than a pass reads is skipped and counted, and the calls after it are still read.
func TestReader_SkipsAnOversizedLine(t *testing.T) {
	t.Parallel()
	q := &recordingQueue{}
	r, path := newTestReader(t, q, "H1")
	huge := `{"type":"user","message":{"content":"` + strings.Repeat("x", maxBytesPerPass+10) + `"}}` + "\n"
	appendTo(t, path, bashLine("toolu_01", "ls")+huge+bashLine("toolu_02", "pwd"))
	var total PassStats
	for range 4 {
		stats := r.RunOnce(context.Background())
		total.Calls += stats.Calls
		total.Unreadable += stats.Unreadable
	}
	assert.Equal(t, PassStats{Calls: 2, Unreadable: 1}, total)
	assert.Equal(t, []string{"toolu_01", "toolu_02"}, q.toolUseIDs())
}

// A transcript replaced by another file of the same size or larger is read from its start, not from where the old one was left.
func TestReader_ReplacementOfAnySizeIsReadFromItsStart(t *testing.T) {
	t.Parallel()
	q := &recordingQueue{}
	r, path := newTestReader(t, q, "H1")
	appendTo(t, path, bashLine("toolu_01", "ls"))
	r.RunOnce(context.Background())
	replacement := path + ".new"
	require.NoError(t, os.WriteFile(replacement, []byte(bashLine("toolu_02", "ps")), 0o600)) // the same length as the first
	require.NoError(t, os.Rename(replacement, path))
	r.RunOnce(context.Background())
	assert.Equal(t, []string{"toolu_01", "toolu_02"}, q.toolUseIDs())
}

// A transcript read again, by a restarted agent, gives each call the event id it had, so the archive keeps it once.
func TestReader_EventIDsAreStable(t *testing.T) {
	t.Parallel()
	q := &recordingQueue{}
	r, path := newTestReader(t, q, "H1")
	appendTo(t, path, bashLine("toolu_01", "ls")+bashLine("toolu_02", "pwd"))
	r.RunOnce(context.Background())
	restarted := New(r.root, r.hostID, q.enqueue, r.logger, capturing)
	restarted.RunOnce(context.Background())
	require.Len(t, q.events, 4)
	assert.Equal(t, q.events[0]["event_id"], q.events[2]["event_id"])
	assert.Equal(t, q.events[1]["event_id"], q.events[3]["event_id"])
	assert.NotEqual(t, q.events[0]["event_id"], q.events[1]["event_id"])
	id, _ := q.events[0]["event_id"].(string)
	parsed, err := uuid.Parse(id)
	require.NoError(t, err)
	assert.Equal(t, id, parsed.String(), "a canonical UUID, as the schema's format asks")
}

// A subagent's calls are in a transcript of its own under the session's directory, carrying the parent session.
func TestReader_ReadsSubagentTranscripts(t *testing.T) {
	t.Parallel()
	q := &recordingQueue{}
	r, path := newTestReader(t, q, "H1")
	sub := filepath.Join(strings.TrimSuffix(path, ".jsonl"), "subagents")
	require.NoError(t, os.MkdirAll(sub, 0o700))
	line := strings.Replace(bashLine("toolu_sub", "echo from-subagent"), `"isSidechain":false`, `"isSidechain":true`, 1)
	require.NoError(t, os.WriteFile(filepath.Join(sub, "agent-a11cf0d792d53f727.jsonl"), []byte(line), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "agent-a11cf0d792d53f727.meta.json"), []byte(`{}`), 0o600))
	assert.Equal(t, PassStats{Calls: 1}, r.RunOnce(context.Background()))
	payload, _ := q.events[0]["payload"].(map[string]any)
	assert.Equal(t, true, payload["sidechain"])
	assert.Equal(t, "47abb2a1-d231-4304-8cb1-52f4460fcd11", payload["agent_session_id"])
}

// A FIFO where the reader expects a directory fails its open at once rather than blocking the pass.
func TestReader_AFIFOForADirectoryDoesNotBlock(t *testing.T) {
	t.Parallel()
	for name, fifo := range map[string]string{
		"among the projects":          filepath.Join(".claude", "projects", "fifo"),
		"in place of the projects":    filepath.Join(".claude", "projects"),
		"in place of a session's dir": filepath.Join(".claude", "projects", "-Users-dev-project", "47abb2a1"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			at := filepath.Join(root, "dev", fifo)
			require.NoError(t, os.MkdirAll(filepath.Dir(at), 0o700))
			require.NoError(t, syscall.Mkfifo(at, 0o600))
			r := New(root, func() string { return "H1" }, (&recordingQueue{}).enqueue, slog.New(slog.NewTextHandler(io.Discard, nil)), capturing)
			done := make(chan struct{})
			go func() { r.RunOnce(context.Background()); close(done) }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("a FIFO blocked the pass")
			}
		})
	}
}

// One pass reads at most maxFilesPerPass transcripts, the most recently changed first.
func TestReader_PassIsBounded(t *testing.T) {
	t.Parallel()
	q := &recordingQueue{}
	r, path := newTestReader(t, q, "H1")
	dir := filepath.Dir(path)
	stale := time.Now().Add(-2 * recentWindow)
	require.NoError(t, os.Chtimes(path, stale, stale)) // the fixture's empty transcript is not one of the many
	newest := time.Now()
	for i := range maxFilesPerPass + 20 {
		session := fmt.Sprintf("s%03d", i)
		name := filepath.Join(dir, session+".jsonl")
		require.NoError(t, os.WriteFile(name, []byte(bashLineIn(session, fmt.Sprintf("toolu_%03d", i), "ls")), 0o600))
		at := newest.Add(-time.Duration(i) * time.Second)
		require.NoError(t, os.Chtimes(name, at, at))
	}
	stats := r.RunOnce(context.Background())
	assert.Equal(t, maxFilesPerPass, stats.Calls)
	assert.Contains(t, q.toolUseIDs(), "toolu_000", "the newest are read")
	assert.NotContains(t, q.toolUseIDs(), fmt.Sprintf("toolu_%03d", maxFilesPerPass+19), "the oldest wait")
	assert.LessOrEqual(t, len(r.files), maxFilesPerPass)
}

// A transcript swapped for a symlink after it was listed is not read through it, even to a file inside the home.
func TestReader_RefusesASymlinkSwappedIn(t *testing.T) {
	t.Parallel()
	q := &recordingQueue{}
	r, path := newTestReader(t, q, "H1")
	other := filepath.Join(filepath.Dir(path), "other.txt")
	require.NoError(t, os.WriteFile(other, []byte(bashLine("toolu_other", "ls")), 0o600))
	listed, err := os.Lstat(path)
	require.NoError(t, err)
	require.NoError(t, os.Remove(path))
	// Relative, so it stays inside the home and os.Root alone would follow it.
	require.NoError(t, os.Symlink(filepath.Base(other), path))
	rel := strings.TrimPrefix(path, filepath.Join(r.root, "dev")+"/")
	read, err := r.readFrom(context.Background(), "H1", transcript{home: "dev", rel: rel, listed: listed}, "k", maxBytesTotalPerPass,
		&PassStats{})
	require.Error(t, err)
	assert.Zero(t, read)
	assert.Empty(t, q.events)
}

// A user writes their own transcripts, so a line naming another session than its transcript's is refused, and a call is stamped
// with the uid of the transcript's owner, which its event id is bound to: another user's forged line can never take its place.
// spec:endpoint-event-collection/agent-tool-call-reporting/a-forged-line-cannot-stand-in-for-another-session-s-call
func TestReader_BindsCallsToTheirTranscript(t *testing.T) {
	t.Parallel()
	q := &recordingQueue{}
	r, path := newTestReader(t, q, "H1")
	appendTo(t, path, bashLineIn("someone-elses-session", "toolu_forged", "ls")+bashLine("toolu_01", "pwd"))
	assert.Equal(t, PassStats{Calls: 1, Unreadable: 1}, r.RunOnce(context.Background()))
	require.Equal(t, []string{"toolu_01"}, q.toolUseIDs())
	payload, _ := q.events[0]["payload"].(map[string]any)
	assert.InDelta(t, float64(os.Getuid()), payload["uid"], 0, "stamped with the transcript owner's uid")

	call := ToolCall{SessionID: "s", ToolUseID: "toolu_01"}
	other := call
	other.OwnerUID = call.OwnerUID + 1
	assert.NotEqual(t, call.EventID(), other.EventID(), "the same session and call under another owner is another event")
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
		require.Len(t, r.files, 1)
		appendTo(t, path, bashLine("toolu_02", "pwd"))
		old := time.Now().Add(-2 * recentWindow)
		require.NoError(t, os.Chtimes(path, old, old))
		assert.Equal(t, PassStats{}, r.RunOnce(context.Background()), "a transcript gone quiet is not read")
	})
}

// A /fork session's job names the session it was forked from (issue #1354): its parent transcript, the parent's transcript at fork
// time, whose lines carry the parent's session id. The reader sends it on the fork's calls. No job, a job naming the session itself,
// a FIFO or a link out of the home in its place, and a subagent's transcript give none.
//
// spec:endpoint-event-collection/fork-parent-from-the-job/a-fork-s-calls-name-the-session-its-job-was-forked-from
func TestReader_ForkParentFromItsJob(t *testing.T) {
	t.Parallel()
	const fork, parent = "47abb2a1-d231-4304-8cb1-52f4460fcd11", "fe7cd1eb-0000-4000-8000-000000001354"
	forkedFrom := func(q *recordingQueue) []any {
		var out []any
		for _, e := range q.events {
			payload, _ := e["payload"].(map[string]any)
			out = append(out, payload["forked_from_session_id"])
		}
		return out
	}
	job := func(t *testing.T, path string) string {
		t.Helper()
		home, _, _ := strings.Cut(path, "/.claude/")
		dir := filepath.Join(home, ".claude", "jobs", fork[:8])
		require.NoError(t, os.MkdirAll(dir, 0o700))
		return filepath.Join(dir, "parent-transcript.jsonl")
	}
	t.Run("the parent its job names", func(t *testing.T) {
		t.Parallel()
		q := &recordingQueue{}
		r, path := newTestReader(t, q, "H1")
		require.NoError(t, os.WriteFile(job(t, path), []byte(`{"type":"file-history-snapshot"}`+"\n"+
			`{"type":"mode","mode":"normal","sessionId":"`+parent+`"}`+"\n"), 0o600))
		appendTo(t, path, bashLine("toolu_01", "ls"))
		r.RunOnce(context.Background())
		appendTo(t, path, bashLine("toolu_02", "pwd"))
		r.RunOnce(context.Background())
		assert.Equal(t, []any{parent, parent}, forkedFrom(q), "on every call, the job read once")
	})
	for name, plant := range map[string]func(t *testing.T, jobPath string){
		"no job": func(*testing.T, string) {},
		"a job naming the session itself": func(t *testing.T, jobPath string) {
			t.Helper()
			require.NoError(t, os.WriteFile(jobPath, []byte(`{"sessionId":"`+fork+`"}`+"\n"), 0o600))
		},
		"a FIFO in the job's place": func(t *testing.T, jobPath string) {
			t.Helper()
			require.NoError(t, syscall.Mkfifo(jobPath, 0o600))
		},
		"a link out of the home": func(t *testing.T, jobPath string) {
			t.Helper()
			outside := filepath.Join(t.TempDir(), "elsewhere.jsonl")
			require.NoError(t, os.WriteFile(outside, []byte(`{"sessionId":"`+parent+`"}`+"\n"), 0o600))
			require.NoError(t, os.Symlink(outside, jobPath))
		},
	} {
		t.Run(name+" names no parent", func(t *testing.T) {
			t.Parallel()
			q := &recordingQueue{}
			r, path := newTestReader(t, q, "H1")
			plant(t, job(t, path))
			appendTo(t, path, bashLine("toolu_01", "ls"))
			done := make(chan struct{})
			go func() { r.RunOnce(context.Background()); close(done) }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("the job read blocked the pass")
			}
			assert.Equal(t, []any{nil}, forkedFrom(q))
		})
	}
	t.Run("a subagent's transcript names none", func(t *testing.T) {
		t.Parallel()
		q := &recordingQueue{}
		r, path := newTestReader(t, q, "H1")
		require.NoError(t, os.WriteFile(job(t, path), []byte(`{"sessionId":"`+parent+`"}`+"\n"), 0o600))
		sub := filepath.Join(strings.TrimSuffix(path, ".jsonl"), "subagents")
		require.NoError(t, os.MkdirAll(sub, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(sub, "agent-a1.jsonl"), []byte(bashLine("toolu_sub", "ls")), 0o600))
		r.RunOnce(context.Background())
		require.Len(t, q.events, 1)
		assert.Equal(t, []any{nil}, forkedFrom(q))
	})
}

// The prompts the user typed are sent beside the tool calls, stamped with the transcript's owner, unless prompt capture is off; a
// prompt naming a session other than its transcript's is passed over, as a call is.
//
// spec:endpoint-event-collection/agent-prompt-capture/a-typed-prompt-is-reported-with-its-turn
// spec:agent-configuration/the-agent-configuration-surface-is-intentionally-minimal/prompt-capture-can-be-turned-off
func TestReader_Prompts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	lines := userLine(`"fix the failing auth test"`, typed) + "\n" + bashLine("toolu_01", "go test ./...") +
		strings.Replace(userLine(`"forged"`, typed), "47abb2a1-d231-4304-8cb1-52f4460fcd11", "5828cd24-efb7-4b7b-a5bf-f3b24681ba10", 1) + "\n"

	t.Run("captured", func(t *testing.T) {
		t.Parallel()
		q := &recordingQueue{}
		r, path := newTestReader(t, q, "H1")
		appendTo(t, path, lines)
		assert.Equal(t, PassStats{Prompts: 1, Calls: 1, Unreadable: 1}, r.RunOnce(ctx), "the forged prompt is passed over")
		require.Len(t, q.events, 2)
		assert.Equal(t, PromptEventType, q.events[0]["event_type"])
		payload, _ := q.events[0]["payload"].(map[string]any)
		assert.Equal(t, "fix the failing auth test", payload["text"])
		assert.Equal(t, "p-1", payload["prompt_id"])
		call, _ := q.events[1]["payload"].(map[string]any)
		assert.Equal(t, "p-1", call["prompt_id"], "the call is made in the turn of the user entry before it")
		info, err := os.Stat(path)
		require.NoError(t, err)
		stat, ok := info.Sys().(*syscall.Stat_t)
		require.True(t, ok)
		assert.InDelta(t, float64(stat.Uid), payload["uid"], 0, "stamped with the transcript's owner")
	})

	t.Run("off", func(t *testing.T) {
		t.Parallel()
		q := &recordingQueue{}
		r, path := newTestReader(t, q, "H1")
		r.prompts = func() bool { return false }
		appendTo(t, path, lines)
		assert.Equal(t, PassStats{Calls: 1}, r.RunOnce(ctx), "no prompt is read, and so none is passed over")
		assert.Equal(t, []string{"toolu_01"}, q.toolUseIDs())
	})

	// The fleet's agent settings can turn capture off while the agent runs: the next line read follows the switch, with no restart.
	//
	// spec:endpoint-event-collection/prompt-capture-follows-the-fleet-agent-settings/turning-capture-off-takes-effect-without-a-restart
	t.Run("turned off between passes", func(t *testing.T) {
		t.Parallel()
		q := &recordingQueue{}
		r, path := newTestReader(t, q, "H1")
		var capture atomic.Bool
		capture.Store(true)
		r.prompts = capture.Load
		appendTo(t, path, userLine(`"first"`, typed)+"\n")
		assert.Equal(t, PassStats{Prompts: 1}, r.RunOnce(ctx))
		capture.Store(false)
		appendTo(t, path, strings.Replace(userLine(`"second"`, typed), `"promptId":"p-1"`, `"promptId":"p-2"`, 1)+"\n")
		assert.Equal(t, PassStats{}, r.RunOnce(ctx), "the prompt typed after capture was turned off is not sent")
		assert.Len(t, q.events, 1)
	})
}

// A call is made in the turn of the user entry before it, whichever kind: a prompt, or a tool result of a turn the agent began for a
// notification. The turn carries over passes, as a transcript is read in pieces.
//
// spec:endpoint-event-collection/agent-prompt-capture/a-typed-prompt-is-reported-with-its-turn
func TestReader_CallsTakeTheirTurn(t *testing.T) {
	t.Parallel()
	q := &recordingQueue{}
	r, path := newTestReader(t, q, "H1")
	ctx := context.Background()
	notified := strings.Replace(userLine(`[{"tool_use_id":"toolu_00","type":"tool_result","content":"ok"}]`, ""), `"promptId":"p-1"`,
		`"promptId":"p-2"`, 1)
	appendTo(t, path, userLine(`"fix it"`, typed)+"\n"+bashLine("toolu_01", "ls"))
	r.RunOnce(ctx)
	appendTo(t, path, bashLine("toolu_02", "pwd")+notified+"\n"+bashLine("toolu_03", "git status"))
	r.RunOnce(ctx)
	turns := map[string]any{}
	for _, e := range q.events {
		if payload, _ := e["payload"].(map[string]any); e["event_type"] == EventType {
			turns[payload["tool_use_id"].(string)] = payload["prompt_id"]
		}
	}
	assert.Equal(t, map[string]any{"toolu_01": "p-1", "toolu_02": "p-1", "toolu_03": "p-2"}, turns)
}

// A user entry of another session in the transcript does not name this session's turn, with prompt capture on or off: the next call
// keeps the turn its own session named.
func TestReader_AForeignUserEntryLeavesTheTurn(t *testing.T) {
	t.Parallel()
	for _, capture := range []bool{true, false} {
		q := &recordingQueue{}
		r, path := newTestReader(t, q, "H1")
		r.prompts = func() bool { return capture }
		foreign := strings.Replace(strings.Replace(userLine(`"not yours"`, typed), "47abb2a1-d231-4304-8cb1-52f4460fcd11",
			"99999999-0000-4000-8000-000000000000", 1), `"promptId":"p-1"`, `"promptId":"p-foreign"`, 1)
		appendTo(t, path, userLine(`"fix it"`, typed)+"\n"+foreign+"\n"+bashLine("toolu_01", "ls"))
		r.RunOnce(context.Background())
		var turn any
		for _, e := range q.events {
			if payload, _ := e["payload"].(map[string]any); e["event_type"] == EventType {
				turn = payload["prompt_id"]
			}
		}
		assert.Equal(t, "p-1", turn, "capture=%v", capture)
	}
}

// spec:endpoint-event-collection/a-resumed-transcript-is-read-from-where-it-stopped/a-resumed-session-sends-only-what-it-added
//
// Issue #1545: a session idle past recentWindow and then resumed had its transcript read from the start again, so every call in it was
// sent again. On a dogfood laptop one 61 MB transcript was sent in full three times in a night.
func TestReader_ResumedTranscriptIsReadFromWhereItStopped(t *testing.T) {
	t.Parallel()
	q := &recordingQueue{}
	r, path := newTestReader(t, q, "H1")
	appendTo(t, path, bashLine("toolu_01", "ls"))
	r.RunOnce(context.Background())
	require.Equal(t, []string{"toolu_01"}, q.toolUseIDs())

	idle := time.Now().Add(-2 * recentWindow)
	require.NoError(t, os.Chtimes(path, idle, idle))
	require.Equal(t, PassStats{}, r.RunOnce(context.Background()), "an idle transcript is not read")

	appendTo(t, path, bashLine("toolu_02", "pwd")) // the session resumes, which also makes the transcript recent again
	stats := r.RunOnce(context.Background())

	assert.Equal(t, 1, stats.Calls, "only the call the session added")
	assert.Equal(t, []string{"toolu_01", "toolu_02"}, q.toolUseIDs(), "the call before the break is not sent again")
}

// fakeFileInfo is a transcript as a pass listed it, changed at modTime.
type fakeFileInfo struct {
	os.FileInfo
	modTime time.Time
}

func (f fakeFileInfo) ModTime() time.Time { return f.modTime }

// spec:endpoint-event-collection/a-resumed-transcript-is-read-from-where-it-stopped/the-positions-kept-are-bounded
func TestReader_TrimPositions(t *testing.T) {
	t.Parallel()
	now := time.Now()

	t.Run("past the bound, the least recently changed are forgotten", func(t *testing.T) {
		t.Parallel()
		r := &Reader{now: func() time.Time { return now }, files: map[string]*position{}}
		for i := range maxPositions + 2 {
			r.files[fmt.Sprintf("t%04d", i)] = &position{offset: 1, latest: fakeFileInfo{modTime: now.Add(-time.Duration(i) * time.Hour)}}
		}
		r.trimPositions()
		assert.Len(t, r.files, maxPositions)
		assert.Contains(t, r.files, "t0000", "the most recently changed is kept")
		assert.Contains(t, r.files, fmt.Sprintf("t%04d", maxPositions-1))
		assert.NotContains(t, r.files, fmt.Sprintf("t%04d", maxPositions), "the two least recently changed are forgotten")
		assert.NotContains(t, r.files, fmt.Sprintf("t%04d", maxPositions+1))
	})

	t.Run("each keeps the most recent messages it sent, at most maxSentMessages", func(t *testing.T) {
		t.Parallel()
		pos := &position{}
		for i := range maxSentMessages + 6 {
			pos.markSent(fmt.Sprintf("msg_%03d", i))
		}
		assert.Len(t, pos.messages, maxSentMessages)
		assert.True(t, pos.sent(fmt.Sprintf("msg_%03d", maxSentMessages+5)), "the most recent is remembered")
		assert.True(t, pos.sent("msg_006"), "and the oldest still within the bound")
		assert.False(t, pos.sent("msg_005"), "the older ones are forgotten")
	})
}

// A transcript the pass budget skips is dated by when it last changed, not by when it was last read, so one still growing is not
// evicted ahead of a transcript truly quieter.
func TestReader_ASkippedTranscriptIsJudgedByItsLatestChange(t *testing.T) {
	t.Parallel()
	q := &recordingQueue{}
	r, path := newTestReader(t, q, "H1")
	dir := filepath.Dir(path)
	newest := time.Now()
	for i := range maxFilesPerPass {
		session := fmt.Sprintf("s%03d", i)
		name := filepath.Join(dir, session+".jsonl")
		require.NoError(t, os.WriteFile(name, []byte(bashLineIn(session, fmt.Sprintf("toolu_%03d", i), "ls")), 0o600))
		require.NoError(t, os.Chtimes(name, newest, newest))
	}
	// The fixture's transcript was read long ago, but has kept changing: it is the 257th most recent, so this pass skips it.
	appendTo(t, path, bashLine("toolu_skipped", "ls"))
	recent := newest.Add(-time.Minute)
	require.NoError(t, os.Chtimes(path, recent, recent))
	key := filepath.Join("dev", ".claude", "projects", "-Users-dev-project", filepath.Base(path))
	lastRead, err := os.Stat(path)
	require.NoError(t, err)
	r.files[key] = &position{file: lastRead, latest: fakeFileInfo{modTime: newest.Add(-2 * recentWindow)}}

	r.RunOnce(context.Background())

	require.NotContains(t, q.toolUseIDs(), "toolu_skipped", "the budget must have skipped it, or this proves nothing")
	assert.Equal(t, recent.Unix(), r.files[key].latest.ModTime().Unix(), "it is dated by its latest change")
}

// A streamed message whose entries land in two passes is sent once.
func TestReader_AMessageStreamedAcrossPassesIsSentOnce(t *testing.T) {
	t.Parallel()
	q := &recordingQueue{}
	r, path := newTestReader(t, q, "H1")
	appendTo(t, path, usageLine("msg_01", "claude-opus-5-5", "")+"\n")
	require.Equal(t, 1, r.RunOnce(context.Background()).Usage)

	appendTo(t, path, strings.Replace(usageLine("msg_01", "claude-opus-5-5", ""), "09:31:00.500Z", "09:31:02.000Z", 1)+"\n")
	assert.Zero(t, r.RunOnce(context.Background()).Usage, "the message's later entry, in the next pass, is not sent again")
}

// A line the queue refused is read again, and may be a later entry of a message whose usage an earlier line sent. Going idle in
// between does not forget that the message was sent.
func TestReader_ARefusedLineReadAfterAnIdleBreakDoesNotResendUsage(t *testing.T) {
	t.Parallel()
	q := &recordingQueue{}
	r, path := newTestReader(t, q, "H1")
	appendTo(t, path, usageLine("msg_01", "claude-opus-5-5", "")+"\n")
	require.Equal(t, 1, r.RunOnce(context.Background()).Usage)

	q.fail = true
	appendTo(t, path, bashLine("toolu_refused", "ls")+
		strings.Replace(usageLine("msg_01", "claude-opus-5-5", ""), "09:31:00.500Z", "09:31:02.000Z", 1)+"\n")
	r.RunOnce(context.Background())
	r.now = func() time.Time { return time.Now().Add(2 * recentWindow) } // the session pauses for longer than the window
	require.Equal(t, PassStats{}, r.RunOnce(context.Background()), "an idle transcript is not read")

	q.fail = false
	r.now = time.Now
	now := time.Now()
	require.NoError(t, os.Chtimes(path, now, now)) // and resumes
	stats := r.RunOnce(context.Background())
	assert.Equal(t, 1, stats.Calls, "the refused call is sent")
	assert.Zero(t, stats.Usage, "and the message whose usage was sent before the break is not sent again")
}

// headlessLine is a user entry of a claude -p session as Claude Code 2.1.321 writes it (issue #1543), trimmed to the fields the
// reader uses: its own id, the entry it follows, and its content. extra is spliced in after the type.
func headlessLine(uuid, parent, content, extra string) string {
	return `{"parentUuid":` + parent + `,"isSidechain":false,"type":"user"` + extra + `,"uuid":"` + uuid + `","promptId":"p-9",` +
		`"sessionId":"47abb2a1-d231-4304-8cb1-52f4460fcd11","timestamp":"2026-11-09T22:43:00.250Z","cwd":"/Users/dev/project",` +
		`"version":"2.1.321","entrypoint":"sdk-cli","userType":"external","message":{"role":"user","content":` + content + `}}` + "\n"
}

// caveatLine is the meta entry Claude Code writes just before it runs a slash command as a local command.
func caveatLine(uuid, entrypoint string) string {
	return strings.Replace(headlessLine(uuid, `"q-1"`, `"<local-command-caveat>Caveat: The messages below were generated by the `+
		`user while running local commands. DO NOT respond to these messages.</local-command-caveat>"`, `,"isMeta":true`),
		`"entrypoint":"sdk-cli"`, `"entrypoint":"`+entrypoint+`"`, 1)
}

// spec:endpoint-event-collection/agent-prompt-capture/a-slash-command-passed-on-the-command-line-is-reported
//
// A slash command passed to claude -p is recorded as Claude Code records a local command: a meta caveat, then the command as plain
// text with neither an origin nor a prompt source. It is the person's prompt only right after its caveat, in a headless session.
func TestReader_HeadlessSlashCommandIsAPrompt(t *testing.T) {
	t.Parallel()
	command := `"/code-review the empty repo"`
	for _, tc := range []struct {
		name  string
		lines string
		want  []string
	}{
		{"a command passed to claude -p, after its caveat",
			caveatLine("cav-1", "sdk-cli") + headlessLine("cmd-1", `"cav-1"`, command, ""), []string{"/code-review the empty repo"}},
		{"the same text without a caveat before it",
			headlessLine("cmd-1", `"q-1"`, command, ""), nil},
		{"the same text after another entry than the caveat",
			caveatLine("cav-1", "sdk-cli") + headlessLine("cmd-1", `"other-entry"`, command, ""), nil},
		{"text after a caveat that is not a slash command",
			caveatLine("cav-1", "sdk-cli") + headlessLine("cmd-1", `"cav-1"`, `"summarize the repo"`, ""), nil},
		{"a command after another meta entry the agent wrote, such as a skill's instructions",
			headlessLine("skill-1", `"q-1"`, `"Base directory for this skill: /Users/dev/.claude/skills/review"`, `,"isMeta":true`) +
				headlessLine("cmd-1", `"skill-1"`, command, ""), nil},
		{"a line the agent wrote itself, which records its own origin",
			caveatLine("cav-1", "sdk-cli") + headlessLine("cmd-1", `"cav-1"`, command, `,"origin":{"kind":"task-notification"}`), nil},
		{"a command after its caveat, with another session's caveat between them",
			caveatLine("cav-1", "sdk-cli") + strings.Replace(caveatLine("cav-foreign", "sdk-cli"),
				`"sessionId":"47abb2a1-d231-4304-8cb1-52f4460fcd11"`, `"sessionId":"99999999-0000-4000-8000-000000000000"`, 1) +
				headlessLine("cmd-1", `"cav-1"`, command, ""), []string{"/code-review the empty repo"}},
		{"an interactive local command such as /clear, which records no origin",
			caveatLine("cav-1", "cli") + strings.Replace(headlessLine("cmd-1", `"cav-1"`,
				`"<command-name>/clear</command-name>\n<command-message>clear</command-message>\n<command-args></command-args>"`, ""),
				`"entrypoint":"sdk-cli"`, `"entrypoint":"cli"`, 1), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			q := &recordingQueue{}
			r, path := newTestReader(t, q, "H1")
			appendTo(t, path, tc.lines)
			r.RunOnce(t.Context())
			var got []string
			for _, e := range q.events {
				if e["event_type"] == PromptEventType {
					payload, _ := e["payload"].(map[string]any)
					got = append(got, payload["text"].(string))
					assert.Equal(t, sdkPromptSource, payload["source"], "a command passed on the command line reads as one")
					assert.Equal(t, "p-9", payload["prompt_id"], "with its own turn, which its work carries too")
				}
			}
			assert.Equal(t, tc.want, got)
		})
	}
}
