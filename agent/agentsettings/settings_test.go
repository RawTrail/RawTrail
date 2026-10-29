package agentsettings

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func writeFile(t *testing.T, dir, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o600))
}

// spec:endpoint-event-collection/prompt-capture-follows-the-fleet-agent-settings/a-restart-keeps-the-settings-last-applied
// spec:endpoint-event-collection/prompt-capture-follows-the-fleet-agent-settings/a-new-agent-waits-for-the-fleet-settings
func TestOpen(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		file  string // empty: no file
		local bool
		want  bool
	}{
		// A new install, or the first start of an agent that applies settings, waits for the fleet's.
		{"a host never sent settings", "", true, false},
		{"settings that turned capture off", `{"version":2,"epoch":5,"capture_prompts":false}`, true, false},
		{"settings that turned capture on", `{"version":3,"epoch":6,"capture_prompts":true}`, true, true},
		// The file may have recorded capture turned off, so a damaged one keeps prompts on the Mac until the next push.
		{"a damaged file", `{"version":`, true, false},

		{"EDR_CAPTURE_PROMPTS=0 under settings that capture", `{"version":3,"epoch":6,"capture_prompts":true}`, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if tc.file != "" {
				writeFile(t, dir, tc.file)
			}
			assert.Equal(t, tc.want, Open(t.Context(), dir, tc.local, discard()).CapturePrompts())
		})
	}

	t.Run("a push of the defaults to a host with none", func(t *testing.T) {
		t.Parallel()
		s := Open(t.Context(), t.TempDir(), true, discard())
		applied, err := s.Apply(Payload{Version: 0, Epoch: 0, CapturePrompts: true})
		require.NoError(t, err)
		assert.True(t, applied, "the first settings are applied whatever their version")
		assert.True(t, s.CapturePrompts())
		applied, err = s.Apply(Payload{Version: 0, Epoch: 0, CapturePrompts: false})
		require.NoError(t, err)
		assert.False(t, applied, "the same version again is not newer")
	})

	t.Run("an unreadable file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		// A directory where the file should be cannot be read as one.
		require.NoError(t, os.Mkdir(filepath.Join(dir, FileName), 0o700))
		assert.False(t, Open(t.Context(), dir, true, discard()).CapturePrompts())
	})
}

// spec:endpoint-event-collection/prompt-capture-follows-the-fleet-agent-settings/turning-capture-off-takes-effect-without-a-restart
// spec:endpoint-event-collection/prompt-capture-follows-the-fleet-agent-settings/an-older-push-does-not-undo-a-newer-one
func TestApply(t *testing.T) {
	t.Parallel()
	off := Payload{Version: 2, Epoch: 100, CapturePrompts: false}
	cases := []struct {
		name        string
		first, then Payload
		wantApplied bool
		wantCapture bool
	}{
		{"a later epoch", off, Payload{Version: 3, Epoch: 200, CapturePrompts: true}, true, true},
		{"a later version at the same epoch", off, Payload{Version: 3, Epoch: 100, CapturePrompts: true}, true, true},
		{"an older push arriving late", off, Payload{Version: 1, Epoch: 50, CapturePrompts: true}, false, false},
		{"the same push again", off, Payload{Version: 2, Epoch: 100, CapturePrompts: true}, false, false},
		// A database restore sends the version backwards and the epoch forwards: the later epoch wins.
		{"a restore's lower version at a later epoch", off, Payload{Version: 1, Epoch: 300, CapturePrompts: true}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			s := Open(t.Context(), dir, true, discard())
			_, err := s.Apply(Payload{Version: 0, Epoch: 0, CapturePrompts: true})
			require.NoError(t, err)
			require.True(t, s.CapturePrompts())
			applied, err := s.Apply(tc.first)
			require.NoError(t, err)
			require.True(t, applied)
			require.False(t, s.CapturePrompts(), "the push turned capture off at once")

			applied, err = s.Apply(tc.then)
			require.NoError(t, err)
			assert.Equal(t, tc.wantApplied, applied)
			assert.Equal(t, tc.wantCapture, s.CapturePrompts())
			assert.Equal(t, tc.wantCapture, Open(t.Context(), dir, true, discard()).CapturePrompts(), "a restart reads what is in force")
		})
	}
}

// The settings read at start order later pushes as the ones applied before the restart did: a late, older push does not undo them.
func TestApply_AfterARestart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, `{"version":3,"epoch":6,"capture_prompts":false}`)
	s := Open(t.Context(), dir, true, discard())
	applied, err := s.Apply(Payload{Version: 2, Epoch: 4, CapturePrompts: true})
	require.NoError(t, err)
	assert.False(t, applied)
	assert.False(t, s.CapturePrompts())
}

func TestApply_LocalOffWins(t *testing.T) {
	t.Parallel()
	s := Open(t.Context(), t.TempDir(), false, discard())
	applied, err := s.Apply(Payload{Version: 1, Epoch: 1, CapturePrompts: true})
	require.NoError(t, err)
	assert.True(t, applied, "the settings are recorded")
	assert.False(t, s.CapturePrompts(), "the server cannot turn on what EDR_CAPTURE_PROMPTS=0 turned off")
}

func TestApply_RefusesANegativeVersion(t *testing.T) {
	t.Parallel()
	s := Open(t.Context(), t.TempDir(), true, discard())
	_, err := s.Apply(Payload{Version: -1, Epoch: 1, CapturePrompts: true})
	require.Error(t, err)
	assert.False(t, s.CapturePrompts(), "nothing changed")
}

// A push that cannot be written still takes effect, and is reported as an error so the server sends it again; the retry then writes it.
func TestApply_WriteFailureTakesEffectAndIsRetried(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := Open(t.Context(), dir, true, discard())
	_, err := s.Apply(Payload{Version: 0, Epoch: 0, CapturePrompts: true})
	require.NoError(t, err)
	require.True(t, s.CapturePrompts())
	// A directory holding a file, where the temporary file goes, can be neither removed nor written as one.
	blocked := filepath.Join(dir, FileName+".tmp")
	require.NoError(t, os.Mkdir(blocked, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(blocked, "x"), nil, 0o600))
	off := Payload{Version: 2, Epoch: 2, CapturePrompts: false}
	applied, err := s.Apply(off)
	require.Error(t, err)
	assert.True(t, applied)
	assert.False(t, s.CapturePrompts(), "capture is off even though it could not be recorded")
	assert.False(t, Open(t.Context(), dir, true, discard()).CapturePrompts(), "a restart does not put the older settings back")

	// The settings not yet written still order later pushes: a late, older one does not turn capture back on.
	applied, err = s.Apply(Payload{Version: 1, Epoch: 1, CapturePrompts: true})
	require.NoError(t, err)
	assert.False(t, applied)
	assert.False(t, s.CapturePrompts())

	require.NoError(t, os.RemoveAll(blocked))
	applied, err = s.Apply(off)
	require.NoError(t, err)
	assert.True(t, applied, "the same push is written on its retry")
	assert.False(t, Open(t.Context(), dir, true, discard()).CapturePrompts())
	applied, err = s.Apply(off)
	require.NoError(t, err)
	assert.False(t, applied, "once written, the same push again is not newer")
}

// The file is the push's payload: what Apply writes, Open reads back.
func TestPayload_RoundTrips(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		want := Payload{
			Version: rapid.Int64Range(1, 1<<40).Draw(t, "version"), Epoch: rapid.Int64().Draw(t, "epoch"),
			CapturePrompts: rapid.Bool().Draw(t, "capture"),
		}
		b, err := json.Marshal(want)
		require.NoError(t, err)
		var got Payload
		require.NoError(t, json.Unmarshal(b, &got))
		assert.Equal(t, want, got)
	})
}
