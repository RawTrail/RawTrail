//go:build !windows

package agenttranscript

import (
	"bytes"
	"context"
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

	"github.com/fleetdm/edr/agent/sensorevent"
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
}

// position is how far one transcript has been read: the offset of the next unread line, the file it was read from, so a transcript
// replaced in place is read from its start, and whether the reader is passing over the rest of a line too long to read.
type position struct {
	offset   int64
	file     os.FileInfo
	skipping bool
}

// New returns a Reader over the home directories under root (/Users on macOS).
func New(root string, hostID func() string, enqueue func(context.Context, []byte) error, logger *slog.Logger) *Reader {
	return &Reader{root: root, hostID: hostID, enqueue: enqueue, now: time.Now, logger: logger, files: map[string]*position{}}
}

// PassStats is what one pass read.
type PassStats struct {
	Calls      int
	Unreadable int
}

// transcript is one recent transcript a pass found: the home it is under, its path within that home, and the regular file the
// listing saw there.
type transcript struct {
	home   string
	rel    string
	listed os.FileInfo
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
					found = append(found, r.recentIn(home, sub, subEntries)...)
				}
			}
		}
		found = append(found, r.recentIn(home, dir, entries)...)
	}
	return found
}

// recentIn returns the transcripts among a directory's entries that changed within recentWindow. An entry's type is its own, not a
// link target's, so a symlink or a FIFO is passed over.
func (r *Reader) recentIn(home, dir string, entries []fs.DirEntry) []transcript {
	var found []transcript
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), transcriptSuffix) {
			continue
		}
		info, err := entry.Info()
		if err != nil || r.now().Sub(info.ModTime()) > recentWindow {
			continue
		}
		found = append(found, transcript{home: home, rel: path.Join(dir, entry.Name()), listed: info})
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
	return int64(len(chunk)), r.consume(ctx, hostID, pos, chunk, stats)
}

// consume handles the complete lines of chunk, read from pos.offset, and advances the position past them. A line that fills a whole
// chunk is too long to wait for: it is counted unreadable and passed over up to its newline, however many passes that takes.
func (r *Reader) consume(ctx context.Context, hostID string, pos *position, chunk []byte, stats *PassStats) error {
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
		if err := r.handleLine(ctx, hostID, chunk[:end], stats); err != nil {
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

// handleLine enqueues one line's tool calls. An unreadable line is counted and passed over; only an enqueue failure stops the read.
func (r *Reader) handleLine(ctx context.Context, hostID string, line []byte, stats *PassStats) error {
	calls, err := ParseLine(line)
	if err != nil {
		stats.Unreadable++
		return nil
	}
	for _, call := range calls {
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
