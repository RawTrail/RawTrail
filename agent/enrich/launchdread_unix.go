//go:build !windows

package enrich

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// plutilPath converts a plist, XML or binary, to XML on its stdout. The agent runs it as the user who ran launchctl, so the kernel
// decides whether that user could read the plist: its permissions, every directory on the way to it, and any ACL.
const (
	plutilPath    = "/usr/bin/plutil"
	plutilTimeout = 2 * time.Second
	// maxConvertedPlistBytes bounds plutil's XML: a binary plist grows when written out as XML, so it gets room past the input bound.
	maxConvertedPlistBytes = 4 * maxPlistBytes
)

// ReadLaunchdJob reads the job a plist defines, as the user who ran launchctl (uid and gid) would. The agent is root and the path is
// the user's, so the read itself runs as that user: plutil, given the user's credentials, opens the path, and a plist the user could
// not have read is refused by the kernel, not judged here. Before that, a stat as root turns away anything that is not a regular file
// of at most maxPlistBytes, so a named pipe or device the user names is never opened for reading, and the timeout bounds a path
// swapped for one since. Supplementary groups are not carried, so a plist readable only through one is passed over.
func ReadLaunchdJob(path string, uid, gid int) (LaunchdJob, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxPlistBytes {
		return LaunchdJob{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), plutilTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, plutilPath, "-convert", "xml1", "-o", "-", "--", path) //nolint:gosec // the user's plist, read as the user
	if uid != os.Geteuid() {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), NoSetGroups: true}} //nolint:gosec // ids from the exec event
	}
	var out bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &out, left: maxConvertedPlistBytes}
	if cmd.Run() != nil {
		return LaunchdJob{}, false
	}
	return parseLaunchdPlist(out.Bytes())
}

// limitedWriter keeps at most left bytes and fails the write past them, so a runaway conversion cannot grow without bound.
type limitedWriter struct {
	w    io.Writer
	left int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if len(p) > l.left {
		return 0, io.ErrShortWrite
	}
	l.left -= len(p)
	return l.w.Write(p)
}
