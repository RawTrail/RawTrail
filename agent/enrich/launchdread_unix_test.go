//go:build !windows

package enrich

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requirePlutil(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(plutilPath); err != nil {
		t.Skip("plutil is macOS's; the plist is read through it")
	}
}

func writePlist(t *testing.T, dir, name string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(jobPlist), 0o600))
	require.NoError(t, os.Chmod(p, mode))
	return p
}

// spec:endpoint-event-collection/launchd-job-definitions/a-plist-the-user-could-not-read-is-not-read-for-them
func TestReadLaunchdJob_AsTheUser(t *testing.T) {
	t.Parallel()
	requirePlutil(t)
	me, myGroup := os.Getuid(), os.Getgid()
	dir := t.TempDir()
	own := writePlist(t, dir, "own.plist", 0o600)
	job, ok := ReadLaunchdJob(own, me, myGroup)
	require.True(t, ok, "a user reads their own plist")
	assert.Equal(t, "com.fleetdm.qa1341.load", job.Label)
	assert.Equal(t, []string{"/bin/sleep", "71"}, job.ProgramArguments)

	link := filepath.Join(dir, "link.plist")
	require.NoError(t, os.Symlink(own, link))
	_, ok = ReadLaunchdJob(link, me, myGroup)
	assert.True(t, ok, "a link is followed as launchctl's open follows it")

	_, ok = ReadLaunchdJob(writePlist(t, dir, "closed.plist", 0o000), me, myGroup)
	assert.False(t, ok, "a plist the user may not read is refused by the kernel")

	// A readable plist in a directory the user cannot enter: the file's own mode allows it, the path does not.
	locked := filepath.Join(dir, "locked")
	require.NoError(t, os.Mkdir(locked, 0o700))
	inside := writePlist(t, locked, "inside.plist", 0o644)
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) }) //nolint:gosec // the test sets its own temp directory's mode
	_, ok = ReadLaunchdJob(inside, me, myGroup)
	assert.False(t, ok, "the directory's permissions count, as they did for launchctl")
}

// Run as root, the agent reads for another user only what that user could read.
func TestReadLaunchdJob_ForAnotherUser(t *testing.T) {
	t.Parallel()
	requirePlutil(t)
	if os.Geteuid() != 0 {
		t.Skip("reading as another user needs the agent's root")
	}
	const owner, other = 1, 2
	dir := t.TempDir()
	private := writePlist(t, dir, "private.plist", 0o600)
	require.NoError(t, os.Chown(private, owner, owner))
	_, ok := ReadLaunchdJob(private, other, other)
	assert.False(t, ok, "another user's private plist")
	_, ok = ReadLaunchdJob(private, owner, owner)
	assert.True(t, ok, "its owner's")
	_, ok = ReadLaunchdJob(writePlist(t, dir, "shared.plist", 0o644), other, other)
	assert.True(t, ok, "a world-readable plist in a directory anyone may enter")
}

// What is not a regular file within the bound is turned away before anything opens it.
func TestReadLaunchdJob_TurnsAwayWhatIsNoPlist(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	me, myGroup := os.Getuid(), os.Getgid()
	big := filepath.Join(dir, "big.plist")
	require.NoError(t, os.WriteFile(big, []byte(strings.Repeat(" ", maxPlistBytes+1)), 0o600))
	for name, path := range map[string]string{"a directory": dir, "an oversized file": big, "a missing file": filepath.Join(dir, "none")} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, ok := ReadLaunchdJob(path, me, myGroup)
			assert.False(t, ok)
		})
	}
	// A named pipe whose writer is held open and sends nothing would stall any read of it; it must return at once.
	fifo := filepath.Join(dir, "pipe.plist")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	writer, err := os.OpenFile(fifo, os.O_RDWR, 0) //nolint:gosec // the test's own pipe
	require.NoError(t, err)
	t.Cleanup(func() { _ = writer.Close() })
	done := make(chan bool, 1)
	go func() { _, ok := ReadLaunchdJob(fifo, me, myGroup); done <- ok }()
	select {
	case ok := <-done:
		assert.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("a held-open named pipe stalled the read")
	}
}

func TestReadLaunchdJob_BinaryPlist(t *testing.T) {
	t.Parallel()
	requirePlutil(t)
	p := writePlist(t, t.TempDir(), "binary.plist", 0o600)
	out, err := exec.CommandContext(t.Context(), plutilPath, "-convert", "binary1", p).CombinedOutput() //nolint:gosec // the test converts its own temp file
	require.NoError(t, err, string(out))
	job, ok := ReadLaunchdJob(p, os.Getuid(), os.Getgid())
	require.True(t, ok)
	assert.Equal(t, "com.fleetdm.qa1341.load", job.Label)
	assert.Equal(t, []string{"/bin/sleep", "71"}, job.ProgramArguments)
}
