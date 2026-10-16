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

// plutilPath converts a binary plist to XML. The plist's bytes go on its stdin, never its path, so it reads what the guard below
// already checked rather than a file that could be swapped since.
const (
	plutilPath    = "/usr/bin/plutil"
	plutilTimeout = 2 * time.Second
	// maxConvertedPlistBytes bounds plutil's XML: a binary plist grows when written out as XML, so it gets room past the input bound.
	maxConvertedPlistBytes = 4 * maxPlistBytes
)

// ReadLaunchdJob reads the job a plist defines for the user who ran launchctl. The agent is root and the path is the user's, so the
// file is read only when the user could have read it: the user is root, owns it with read permission, or it is world-readable.
// Opening first and checking the open file means a path swapped to point elsewhere is judged by what it points at, and the
// non-blocking open keeps a named pipe from stalling it. Whatever is read is bounded by maxPlistBytes and only ever parsed as a
// plist, so a directory, pipe or device yields no job. Group permission is not checked, so a group-only file is passed over.
func ReadLaunchdJob(path string, uid int) (LaunchdJob, bool) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0) //nolint:gosec // reading the user's plist is the point; the open file is checked before a byte is read
	if err != nil {
		return LaunchdJob{}, false
	}
	defer func() { _ = f.Close() }() // read only: a failed close loses nothing
	info, err := f.Stat()
	if err != nil || info.Size() > maxPlistBytes || !readableBy(info, uid) {
		return LaunchdJob{}, false
	}
	data, err := io.ReadAll(io.LimitReader(f, maxPlistBytes+1))
	if err != nil || len(data) > maxPlistBytes {
		return LaunchdJob{}, false
	}
	if bytes.HasPrefix(data, []byte(binaryPlistPrefix)) {
		if data, err = binaryToXML(data); err != nil {
			return LaunchdJob{}, false
		}
	}
	return parseLaunchdPlist(data)
}

// readableBy reports whether uid could read the file: root reads anything, an owner needs the owner read bit, anyone else the
// world read bit.
func readableBy(info os.FileInfo, uid int) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	perm := info.Mode().Perm()
	return uid == 0 || (int(st.Uid) == uid && perm&0o400 != 0) || perm&0o004 != 0
}

// binaryToXML converts a binary plist's bytes to XML through plutil, bounded in time and in output.
func binaryToXML(data []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), plutilTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, plutilPath, "-convert", "xml1", "-o", "-", "--", "-")
	cmd.Stdin = bytes.NewReader(data)
	var out bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &out, left: maxConvertedPlistBytes}
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
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
