//go:build !windows

package agenttranscript

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/rawtrail/rawtrail/agent/sensorevent"
)

const (
	// defaultInterval is how often the transcripts are read. A tool call's process is on the wire within a second; the call itself
	// may trail it by this much, which only delays the label beside it.
	defaultInterval = 5 * time.Second
	// recentWindow is how long after its last change a transcript is still read. Older transcripts belong to finished sessions, and
	// the window also bounds what is kept per transcript: a file outside it is forgotten.
	recentWindow = 10 * time.Minute
	// maxBytesPerPass bounds what one pass reads from one transcript, so a long session first seen after a restart is caught up over
	// several passes rather than in one long one. A line longer than this is skipped, not waited on.
	maxBytesPerPass = 4 << 20
	// maxFilesPerPass and maxBytesTotalPerPass bound one pass over every home, so transcripts cannot take the agent away from the
	// telemetry it exists for: the most recently changed transcripts are read first, and the rest wait for a later pass.
	maxFilesPerPass      = 256
	maxBytesTotalPerPass = 16 << 20
	// projectsDir is where Claude Code keeps its transcripts under a home directory, one directory per project.
	projectsDir = ".claude/projects"
	// subagentsDir holds a session's subagent transcripts: <project>/<session id>/subagents/agent-<id>.jsonl, whose lines carry the
	// parent session's id (Claude Code 2.1.296, probed on edr-dev).
	subagentsDir = "subagents"
	// transcriptSuffix names a transcript within a project directory.
	transcriptSuffix = ".jsonl"
	// jobsDir holds Claude Code's background session jobs (2.1.301 and later), each named for the first jobIDLen characters of its
	// session id. A /fork's job holds parentTranscript, its parent's transcript at fork time, which the fork resumes from, so its
	// lines carry the parent's session id: the fork's immediate parent, probed on edr-dev (issue #1354).
	jobsDir          = ".claude/jobs"
	jobIDLen         = 8
	parentTranscript = "parent-transcript.jsonl"
	// maxParentProbeBytes bounds the read of a job's parent transcript: its first lines name the parent, and the rest is not read.
	maxParentProbeBytes = 256 << 10
	// maxSessionIDLen bounds a session id read from a file, as the server's column does.
	maxSessionIDLen = 256
)

// Reader tails the agent transcripts under a root of home directories and enqueues each tool call as an agent_tool_call event.
//
// Everything under a home directory is the user's, and the agent reads it as root, so each home is opened as an os.Root: a symlink
// anywhere on the way to a transcript, .claude and the project directory included, resolves only within that home, and a path that
// would leave it is refused.
//
// Positions live in memory only. A restart reads each recent transcript from its start again; each call's event id is derived from
// its session and tool_use_id, so the archive keeps it once.
type Reader struct {
	root    string
	hostID  func() string
	enqueue func(context.Context, []byte) error
	now     func() time.Time
	logger  *slog.Logger
	files   map[string]*position
	// prompts is whether the prompts the user typed are read and sent, as well as the tool calls (issue #1426).
	prompts bool
}

// position is how far one transcript has been read: the offset of the next unread line, the file it was read from, so a transcript
// replaced in place is read from its start, and whether the reader is passing over the rest of a line too long to read.
type position struct {
	offset   int64
	file     os.FileInfo
	skipping bool
	// parent is the session the transcript's session was forked from, read once from its job's parent transcript (issue #1354).
	parent     string
	parentRead bool
	// turn is the turn the last user entry read belongs to, which each tool call after it was made in (issue #1426). Measured on 60 real
	// transcripts, it names the same turn as the call's own result in every one of 46,833 calls.
	turn string
}

// New returns a Reader over the home directories under root (/Users on macOS). capturePrompts sends the prompts the user typed as well
// as the tool calls.
func New(root string, hostID func() string, enqueue func(context.Context, []byte) error, logger *slog.Logger, capturePrompts bool) *Reader {
	return &Reader{root: root, hostID: hostID, enqueue: enqueue, now: time.Now, logger: logger, files: map[string]*position{},
		prompts: capturePrompts}
}

// PassStats is what one pass read.
type PassStats struct {
	Calls      int
	Prompts    int
	Unreadable int
}

// transcript is one recent transcript a pass found: the home it is under, its path within that home, the regular file the listing saw
// there, and the session its path names, which every line of it must carry.
type transcript struct {
	home    string
	rel     string
	listed  os.FileInfo
	session string
}

// Run reads the transcripts every defaultInterval until ctx ends.
func (r *Reader) Run(ctx context.Context) {
	ticker := time.NewTicker(defaultInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if stats := r.RunOnce(ctx); stats.Unreadable > 0 {
				r.logger.WarnContext(ctx, "agent transcript lines skipped", "edr.transcript.unreadable", stats.Unreadable,
					"edr.transcript.calls", stats.Calls)
			}
		}
	}
}

// RunOnce reads what the recent transcripts gained since the last pass, the most recently changed first, within the pass's budgets.
// Without a host id there is nothing to attribute a call to, so the pass is skipped and the positions stay where they are.
func (r *Reader) RunOnce(ctx context.Context) PassStats {
	var stats PassStats
	hostID := r.hostID()
	if hostID == "" {
		return stats
	}
	homes, err := os.ReadDir(r.root)
	if err != nil {
		return stats
	}
	var found []transcript
	for _, home := range homes {
		if home.IsDir() {
			found = append(found, r.listHome(home.Name())...)
		}
	}
	slices.SortFunc(found, func(a, b transcript) int { return b.listed.ModTime().Compare(a.listed.ModTime()) })
	kept := map[string]bool{}
	budget := int64(maxBytesTotalPerPass)
	for i, t := range found {
		if i == maxFilesPerPass {
			break
		}
		key := path.Join(t.home, t.rel)
		kept[key] = true
		if ctx.Err() != nil || budget <= 0 {
			continue
		}
		read, err := r.readFrom(ctx, hostID, t, key, budget, &stats)
		budget -= read
		if err != nil {
			r.logger.WarnContext(ctx, "read agent transcript", "err", err)
		}
	}
	for key := range r.files {
		if !kept[key] {
			delete(r.files, key)
		}
	}
	return stats
}

// listHome lists the recent transcripts under one home directory, sessions' and their subagents', through an os.Root confined to it.
func (r *Reader) listHome(home string) []transcript {
	homeRoot, err := os.OpenRoot(path.Join(r.root, home))
	if err != nil {
		return nil
	}
	defer func() { _ = homeRoot.Close() }() // read only: a failed close loses nothing
	projects, err := readDir(homeRoot, projectsDir)
	if err != nil {
		return nil
	}
	var found []transcript
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		dir := path.Join(projectsDir, project.Name())
		entries, err := readDir(homeRoot, dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				// A session directory holds its subagents' transcripts.
				sub := path.Join(dir, entry.Name(), subagentsDir)
				if subEntries, err := readDir(homeRoot, sub); err == nil {
					found = append(found, r.recentIn(home, sub, subEntries, entry.Name())...)
				}
			}
		}
		found = append(found, r.recentIn(home, dir, entries, "")...)
	}
	return found
}

// recentIn returns the transcripts among a directory's entries that changed within recentWindow. An entry's type is its own, not a
// link target's, so a symlink or a FIFO is passed over. A session's transcript is named for the session; a subagent's is under the
// session's directory, whose name session carries.
func (r *Reader) recentIn(home, dir string, entries []fs.DirEntry, session string) []transcript {
	var found []transcript
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), transcriptSuffix) {
			continue
		}
		info, err := entry.Info()
		if err != nil || r.now().Sub(info.ModTime()) > recentWindow {
			continue
		}
		named := session
		if named == "" {
			named = strings.TrimSuffix(entry.Name(), transcriptSuffix)
		}
		found = append(found, transcript{home: home, rel: path.Join(dir, entry.Name()), listed: info, session: named})
	}
	return found
}

// readDir lists a directory within a home. O_DIRECTORY and O_NONBLOCK make the open fail at once for anything else the user put there,
// a FIFO included, which a plain open would block on.
func readDir(homeRoot *os.Root, name string) ([]fs.DirEntry, error) {
	f, err := homeRoot.OpenFile(name, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read only: a failed close loses nothing
	return f.ReadDir(-1)
}

// readFrom reads one transcript from its position, at most budget bytes, and enqueues the tool calls of each complete line, advancing
// past each line it handled; it returns how much it read. A trailing line without its newline is still being written and waits for
// the next pass. A transcript that is not the file last read, or that is shorter than the position, was replaced and is read from its
// start.
func (r *Reader) readFrom(ctx context.Context, hostID string, t transcript, key string, budget int64, stats *PassStats) (int64, error) {
	homeRoot, err := os.OpenRoot(path.Join(r.root, t.home))
	if err != nil {
		return 0, fmt.Errorf("open home: %w", err)
	}
	defer func() { _ = homeRoot.Close() }() // read only: a failed close loses nothing
	// The transcript was a regular file when listed, but its directory is the user's, who can swap something else in since.
	// O_NONBLOCK keeps a FIFO from blocking the open, and the identity check after it turns away anything that is not the file the
	// listing saw: os.Root follows a symlink that stays inside the home, O_NOFOLLOW or not.
	f, err := homeRoot.OpenFile(t.rel, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return 0, fmt.Errorf("open transcript: %w", err)
	}
	defer func() { _ = f.Close() }() // read only: a failed close loses nothing
	info, err := f.Stat()
	if err != nil || !os.SameFile(t.listed, info) {
		return 0, fmt.Errorf("transcript %s changed since it was listed", path.Base(t.rel))
	}
	pos := r.files[key]
	if pos == nil || !os.SameFile(pos.file, info) || info.Size() < pos.offset {
		pos = &position{}
		r.files[key] = pos
	}
	pos.file = info
	if info.Size() == pos.offset {
		return 0, nil
	}
	chunk, err := io.ReadAll(io.LimitReader(io.NewSectionReader(f, pos.offset, info.Size()-pos.offset), min(maxBytesPerPass, budget)))
	if err != nil {
		return 0, fmt.Errorf("read transcript: %w", err)
	}
	if !pos.parentRead && !strings.Contains(t.rel, "/"+subagentsDir+"/") {
		pos.parent, pos.parentRead = forkParent(homeRoot, t.session), true
	}
	src := source{session: t.session, forkedFrom: pos.parent}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		src.ownerUID = st.Uid
	}
	return int64(len(chunk)), r.consume(ctx, hostID, src, pos, chunk, stats)
}

// source is what a transcript's lines are checked and stamped against: the session its path names, the uid that owns it, and the
// session it was forked from, if its job names one.
type source struct {
	session    string
	ownerUID   uint32
	forkedFrom string
}

// forkParent returns the session a /fork session was forked from, read from the first line of its job's parent transcript that names
// a session, or "" when the session has no such job (Claude Code before 2.1.301, or a session not started by /fork). The job is the
// user's file, read within their home's os.Root as the transcripts are, O_NONBLOCK and only as a regular file, and at most
// maxParentProbeBytes of it.
func forkParent(homeRoot *os.Root, session string) string {
	if len(session) < jobIDLen {
		return ""
	}
	f, err := homeRoot.OpenFile(path.Join(jobsDir, session[:jobIDLen], parentTranscript), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }() // read only: a failed close loses nothing
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return ""
	}
	lines := bufio.NewScanner(io.LimitReader(f, maxParentProbeBytes))
	lines.Buffer(nil, maxParentProbeBytes)
	for lines.Scan() {
		var line struct {
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(lines.Bytes(), &line) != nil || line.SessionID == "" {
			continue
		}
		if line.SessionID == session || len(line.SessionID) > maxSessionIDLen {
			return ""
		}
		return line.SessionID
	}
	return ""
}

// consume handles the complete lines of chunk, read from pos.offset, and advances the position past them. A line that fills a whole
// chunk is too long to wait for: it is counted unreadable and passed over up to its newline, however many passes that takes.
func (r *Reader) consume(ctx context.Context, hostID string, src source, pos *position, chunk []byte, stats *PassStats) error {
	if pos.skipping {
		end := bytes.IndexByte(chunk, '\n')
		if end < 0 {
			pos.offset += int64(len(chunk))
			return nil
		}
		pos.skipping = false
		pos.offset += int64(end) + 1
		chunk = chunk[end+1:]
	}
	for {
		end := bytes.IndexByte(chunk, '\n')
		if end < 0 {
			break
		}
		if err := r.handleLine(ctx, hostID, src, pos, chunk[:end], stats); err != nil {
			// The line is read again next pass: a call the queue refused is not lost.
			return err
		}
		pos.offset += int64(end) + 1
		chunk = chunk[end+1:]
	}
	if len(chunk) == maxBytesPerPass {
		stats.Unreadable++
		pos.skipping = true
		pos.offset += int64(len(chunk))
	}
	return nil
}

// handleLine enqueues one line's tool calls, and its prompt when prompts are captured, stamped with the transcript's owner. An
// unreadable line, or one naming a session other than its transcript's, is counted and passed over: the file is the user's to write,
// and a line naming another session would put a call or a prompt in it that never happened there. Only an enqueue failure stops the
// read.
func (r *Reader) handleLine(ctx context.Context, hostID string, src source, pos *position, raw []byte, stats *PassStats) error {
	line, err := ParseLine(raw)
	if err != nil {
		stats.Unreadable++
		return nil
	}
	// A user entry of another session neither names this session's turn nor carries a prompt of it.
	if line.Turn != "" && line.TurnSession == src.session {
		pos.turn = line.Turn
	}
	if prompt := line.Prompt; prompt != nil && r.prompts {
		if prompt.SessionID != src.session {
			stats.Unreadable++
			return nil
		}
		prompt.OwnerUID = src.ownerUID
		body, err := sensorevent.Marshal(prompt.EventID(), hostID, PromptEventType, prompt.At.UnixNano(), prompt.Payload())
		if err != nil {
			return err
		}
		if err := r.enqueue(ctx, body); err != nil {
			return fmt.Errorf("enqueue prompt: %w", err)
		}
		stats.Prompts++
	}
	calls := line.Calls
	for _, call := range calls {
		if call.SessionID != src.session {
			stats.Unreadable++
			return nil
		}
	}
	for _, call := range calls {
		call.OwnerUID, call.ForkedFromSessionID, call.PromptID = src.ownerUID, src.forkedFrom, pos.turn
		body, err := sensorevent.Marshal(call.EventID(), hostID, EventType, call.At.UnixNano(), call.Payload())
		if err != nil {
			return err
		}
		if err := r.enqueue(ctx, body); err != nil {
			return fmt.Errorf("enqueue tool call: %w", err)
		}
		stats.Calls++
	}
	return nil
}
