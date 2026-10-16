//go:build !windows

package enrich

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The tools a plist is read with, both run as the user who ran launchctl. head reads at most one byte past the bound, so the input is
// capped where it is read, whatever the file grows to; plutil converts a binary plist's bytes from stdin and never opens a path.
const (
	headPath    = "/usr/bin/head"
	findPath    = "/usr/bin/find"
	plutilPath  = "/usr/bin/plutil"
	readTimeout = 2 * time.Second
	// maxConvertedPlistBytes bounds plutil's XML: a binary plist grows when written out as XML, so it gets room past the input bound.
	maxConvertedPlistBytes = 4 * maxPlistBytes
	binaryPlistPrefix      = "bplist00"
	// maxListingBytes bounds a directory's listing: room for a thousand plists, and a directory past it is read as none.
	maxListingBytes = 64 << 10
)

// ReadLaunchdJobs reads the jobs a launchctl operand names, as the user who ran launchctl: the job the plist defines, or, when the
// operand is not one, the jobs of the .plist files directly in it as a directory (issue #1352), which `launchctl load <dir>` loads.
// The plist read comes first because launchctl takes a plist of any name. The directory is listed as the user, as each plist is read,
// so the listing is no wider than theirs: find, run with their uid and gid and no supplementary groups, prints the names, at most
// maxListingBytes of them, and the first maxLaunchdPlists in name order are read. -H follows a symlinked directory operand, as
// launchctl's own open does. The path is absolute (LaunchdJobs makes it so), so find cannot take it for an option.
func ReadLaunchdJobs(path string, uid, gid int) []LaunchdJob {
	if job, ok := ReadLaunchdJob(path, uid, gid); ok {
		job.PlistPath = path
		return []LaunchdJob{job}
	}
	listing, err := runAs(uid, gid, nil, maxListingBytes, findPath, "-H", path, "-mindepth", "1", "-maxdepth", "1", "-name", "*.plist",
		"-print0")
	if err != nil || len(listing) == 0 {
		return nil
	}
	plists := strings.Split(strings.TrimSuffix(string(listing), "\x00"), "\x00")
	slices.Sort(plists)
	var jobs []LaunchdJob
	for _, plist := range plists[:min(len(plists), maxLaunchdPlists)] {
		if job, ok := ReadLaunchdJob(plist, uid, gid); ok {
			job.PlistPath = plist
			jobs = append(jobs, job)
		}
	}
	return jobs
}

// ReadLaunchdJob reads the job a plist defines, as the user who ran launchctl (uid and gid) would. The agent is root and the path is
// the user's, so nothing here opens it as root: head, run with the user's uid and gid and no supplementary groups, reads it, and the
// kernel decides whether that user could, through every directory on the way, the file's mode and any ACL. A plist readable only
// through a supplementary group is passed over. head stops one byte past maxPlistBytes, so an oversized file is refused without
// being read whole, and the timeout ends a read that blocks, a named pipe or a stalled mount among them.
func ReadLaunchdJob(path string, uid, gid int) (LaunchdJob, bool) {
	data, err := runAs(uid, gid, nil, maxPlistBytes+1, headPath, "-c", strconv.Itoa(maxPlistBytes+1), "--", path)
	if err != nil || len(data) > maxPlistBytes {
		return LaunchdJob{}, false
	}
	if bytes.HasPrefix(data, []byte(binaryPlistPrefix)) {
		if data, err = runAs(uid, gid, data, maxConvertedPlistBytes, plutilPath, "-convert", "xml1", "-o", "-", "--", "-"); err != nil {
			return LaunchdJob{}, false
		}
	}
	return parseLaunchdPlist(data)
}

// runAs runs a tool as uid and gid with no supplementary groups, feeding it stdin, keeping at most limit bytes of its output, and
// ending it at readTimeout. The agent's own groups are cleared rather than inherited: a group root holds must not open a file for a
// user who is not in it. When the agent already runs as uid, as the tests do, no switch is made.
func runAs(uid, gid int, stdin []byte, limit int, tool string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	//nolint:gosec // tool is head or plutil, both fixed paths; the user's path is only an argument, read as the user
	cmd := exec.CommandContext(ctx, tool, args...)
	cmd.WaitDelay = readTimeout
	if uid != os.Geteuid() {
		cred := &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: []uint32{}} //nolint:gosec // ids from the exec event
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: cred}
	}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &out, left: limit}
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// limitedWriter keeps at most left bytes and fails the write past them, so a runaway tool cannot grow its output without bound.
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
