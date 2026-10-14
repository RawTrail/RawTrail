//go:build !windows

package agenttranscript

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fleetdm/edr/agent/sensorevent"
)

const (
	// defaultInterval is how often the transcripts are read. A tool call's process is on the wire within a second; the call itself
	// may trail it by this much, which only delays the label beside it.
	defaultInterval = 5 * time.Second
	// recentWindow is how long after its last change a transcript is still read. Older transcripts belong to finished sessions, and
	// the window also bounds the offsets kept: a file outside it is forgotten.
	recentWindow = 10 * time.Minute
	// maxBytesPerPass bounds what one pass reads from one transcript, so a long session first seen after a restart is caught up over
	// several passes rather than in one long one.
	maxBytesPerPass = 4 << 20
	// transcriptGlob matches Claude Code's transcripts under each home directory of the root.
	transcriptGlob = "*/.claude/projects/*/*.jsonl"
)

// Reader tails the agent transcripts under a root of home directories and enqueues each tool call as an agent_tool_call event.
//
// Offsets live in memory only. A restart reads each recent transcript from its start again, and the server keys a call on its
// tool_use_id, so the repeat changes nothing.
type Reader struct {
	root    string
	hostID  func() string
	enqueue func(context.Context, []byte) error
	now     func() time.Time
	logger  *slog.Logger
	offsets map[string]int64
}

// New returns a Reader over the home directories under root (/Users on macOS).
func New(root string, hostID func() string, enqueue func(context.Context, []byte) error, logger *slog.Logger) *Reader {
	return &Reader{root: root, hostID: hostID, enqueue: enqueue, now: time.Now, logger: logger, offsets: map[string]int64{}}
}

// PassStats is what one pass read.
type PassStats struct {
	Calls      int
	Unreadable int
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

// RunOnce reads what each recent transcript gained since the last pass. Without a host id there is nothing to attribute a call to,
// so the pass is skipped and the offsets stay where they are.
func (r *Reader) RunOnce(ctx context.Context) PassStats {
	var stats PassStats
	hostID := r.hostID()
	if hostID == "" {
		return stats
	}
	paths, err := filepath.Glob(filepath.Join(r.root, transcriptGlob))
	if err != nil {
		// Only a malformed pattern fails a glob, and the pattern is a constant.
		return stats
	}
	recent := map[string]bool{}
	for _, path := range paths {
		// Lstat, not Stat: the agent reads as root under directories users own, so a symlink planted as a transcript is not followed.
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || r.now().Sub(info.ModTime()) > recentWindow {
			continue
		}
		recent[path] = true
		if err := r.readFrom(ctx, hostID, path, info.Size(), &stats); err != nil {
			r.logger.WarnContext(ctx, "read agent transcript", "err", err)
		}
	}
	for path := range r.offsets {
		if !recent[path] {
			delete(r.offsets, path)
		}
	}
	return stats
}

// readFrom reads path from its offset and enqueues the tool calls of each complete line, advancing the offset past each line it
// handled. A trailing line without its newline is still being written and waits for the next pass; a file shorter than its offset
// was replaced and is read from its start.
func (r *Reader) readFrom(ctx context.Context, hostID, path string, size int64, stats *PassStats) error {
	offset := r.offsets[path]
	if size < offset {
		offset = 0
	}
	if size == offset {
		r.offsets[path] = offset
		return nil
	}
	// The path was a regular file when listed, but its directory is the user's: O_NOFOLLOW refuses a symlink swapped in since, and
	// O_NONBLOCK keeps a FIFO from blocking the open, which the check after it then turns away.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0) // #nosec G304 -- a listed transcript, checked below
	if err != nil {
		return fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("transcript %s is not a regular file", filepath.Base(path))
	}
	chunk, err := io.ReadAll(io.LimitReader(io.NewSectionReader(f, offset, size-offset), maxBytesPerPass))
	if err != nil {
		return fmt.Errorf("read transcript: %w", err)
	}
	for {
		end := bytes.IndexByte(chunk, '\n')
		if end < 0 {
			break
		}
		if err := r.handleLine(ctx, hostID, chunk[:end], stats); err != nil {
			// The line is read again next pass: a call the queue refused is not lost.
			r.offsets[path] = offset
			return err
		}
		offset += int64(end) + 1
		chunk = chunk[end+1:]
	}
	r.offsets[path] = offset
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
		body, err := sensorevent.Marshal(hostID, EventType, call.At.UnixNano(), call.Payload())
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
