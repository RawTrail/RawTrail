// Package agentsettings holds the fleet-wide agent settings the server pushes with set_agent_settings (issue #1426): today whether the
// prompts people type to AI agents are captured. An agent captures nothing until it has the fleet's settings, so a fleet that turned
// capture off never has prompts sent by an agent that has not heard yet; the server sends them to every host, unchanged ones
// included. The last settings applied are kept in a file beside the event queue, so a restart honours them at once.
package agentsettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// FileName is the settings file's name in the agent's state directory.
const FileName = "agent-settings.json"

// Payload is a set_agent_settings payload, and what the file holds.
type Payload struct {
	Version        int64 `json:"version"`
	Epoch          int64 `json:"epoch"`
	CapturePrompts bool  `json:"capture_prompts"`
}

// newer reports whether p is later than applied. Settings are ordered by epoch, their update time, and then by version, so a database
// restore that sends the version backwards still moves forward.
func (p Payload) newer(applied Payload) bool {
	return p.Epoch > applied.Epoch || (p.Epoch == applied.Epoch && p.Version > applied.Version)
}

// Settings are the settings in force on this host.
type Settings struct {
	path string
	// local is the agent's own EDR_CAPTURE_PROMPTS. When it is off the server cannot turn capture on.
	local bool

	mu sync.Mutex
	// known is whether the agent has the fleet's settings, from the file or a push; applied is what they are.
	known   bool
	applied Payload
	capture atomic.Bool
}

// Open reads the settings last applied from dir. With none, as on a new install or the first start of an agent that applies settings,
// prompts are not captured until the server sends the fleet's. A file that cannot be read or decoded is treated the same way, since it
// may have recorded an administrator turning capture off.
func Open(ctx context.Context, dir string, localCapture bool, logger *slog.Logger) *Settings {
	s := &Settings{path: filepath.Join(dir, FileName), local: localCapture}
	capture := false
	b, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		logger.WarnContext(ctx, "agent settings: cannot read the settings file; prompt capture is off until the server sends them",
			"path", s.path, "err", err)
	case json.Unmarshal(b, &s.applied) != nil:
		logger.WarnContext(ctx, "agent settings: the settings file is damaged; prompt capture is off until the server sends them", "path", s.path)
		s.applied = Payload{}
	default:
		s.known, capture = true, s.applied.CapturePrompts
	}
	s.capture.Store(localCapture && capture)
	return s
}

// CapturePrompts reports whether prompts are captured now. Safe to call from any goroutine.
func (s *Settings) CapturePrompts() bool {
	return s.capture.Load()
}

// Apply puts a set_agent_settings payload in force when the agent has no settings yet or it is newer than those applied, and reports
// whether it was. Commands can arrive out of order, so an older one is acknowledged and ignored. Version 0 is the settings no operator
// has changed. The settings take effect even when they cannot be written to
// the file, which returns an error so the command is retried; the settings applied are then left as they were, so the retry writes
// them.
func (s *Settings) Apply(p Payload) (bool, error) {
	if p.Version < 0 {
		return false, fmt.Errorf("invalid version %d", p.Version)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.known && !p.newer(s.applied) {
		return false, nil
	}
	s.capture.Store(s.local && p.CapturePrompts)
	if err := s.write(p); err != nil {
		return true, err
	}
	s.known, s.applied = true, p
	return true, nil
}

// write replaces the file with p, through a temporary file renamed over it, so a crash leaves the old settings or the new ones and
// never half of either. Marshalling a struct of integers and a boolean cannot fail.
func (s *Settings) write(p Payload) error {
	b, _ := json.Marshal(p)
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write agent settings: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("write agent settings: %w", err)
	}
	return nil
}
