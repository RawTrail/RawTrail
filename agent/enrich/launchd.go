package enrich

import (
	"encoding/json"
	"path"
	"strings"
	"time"
)

// Launchd job definitions (issue #1341). `launchctl load <plist>` and `launchctl bootstrap <domain> <plist>` name a file, and the
// job it defines (its label, and the program it runs) is inside it. The server needs the label to link the command to the job
// launchd starts, and a persistence detection wants the program, so the agent reads each named plist when it sees the exec and
// sends the definitions with it.

const (
	launchctlPath = "/bin/launchctl"
	// maxLaunchdPlists bounds the plists one exec is read for: `launchctl load` takes any number of paths.
	maxLaunchdPlists = 8
)

// LaunchdJob is one job definition a launchctl exec loaded, as its plist states it. Truncated reports that a field was cut to the
// read's bounds.
type LaunchdJob struct {
	PlistPath        string   `json:"plist_path"`
	Label            string   `json:"label,omitempty"`
	Program          string   `json:"program,omitempty"`
	ProgramArguments []string `json:"program_arguments,omitempty"`
	Truncated        bool     `json:"truncated,omitempty"`
}

// JobReader reads the jobs one launchctl operand names, as the user who ran launchctl (uid and gid): the job the plist defines, or
// for a directory those of the plists directly in it (issue #1352), each with its PlistPath. None when that user could not have read
// them or they do not parse as jobs. Production passes ReadLaunchdJobs; tests inject a fake.
type JobReader func(path string, uid, gid int) []LaunchdJob

// BoundedReader returns read bounded for the agent's event loop, which handles every event in turn: the loop waits at most wait for a
// read, which runs on its own goroutine, and at most one read runs at a time, so a plist that blocks (a stalled mount, a pipe) costs
// the loop at most wait once, and any read asked for while it is still running returns at once with nothing. The exec then goes out
// without the definitions it could not read in time, rather than holding back the telemetry behind it.
func BoundedReader(read JobReader, wait time.Duration) JobReader {
	slot := make(chan struct{}, 1)
	return func(path string, uid, gid int) []LaunchdJob {
		select {
		case slot <- struct{}{}:
		default:
			return nil
		}
		done := make(chan []LaunchdJob, 1)
		go func() {
			jobs := read(path, uid, gid)
			// The slot is freed before the result is sent, so a caller that has it can read the next operand at once.
			<-slot
			done <- jobs
		}()
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case jobs := <-done:
			return jobs
		case <-timer.C:
			return nil
		}
	}
}

// LaunchdJobs returns data with a `launchctl load|bootstrap` exec's payload carrying launchd_jobs, the definitions of the plists it
// named that the reader could read, at most maxLaunchdPlists across its operands, a directory's plists included, or data unchanged
// for any other event, an exec already carrying them, or one naming none it could read. Paths are macOS's, so they are read as POSIX
// paths whatever the build. A relative path is the launcher's: it is read under the exec's working directory as it was given, `..`
// included, so a symlinked directory resolves the way launchctl's own open did, and is skipped without one. Every path the reader
// is asked for is therefore absolute.
func LaunchdJobs(data []byte, read JobReader) []byte {
	envelope, payload, ok := decodeEvent(data, "exec")
	if !ok || hasField(payload, "launchd_jobs") {
		return data
	}
	var exec struct {
		Path string   `json:"path"`
		Args []string `json:"args"`
		// uint32, the width of a uid: a value outside it fails the decode, rather than wrapping to another user (0, root, among them).
		UID uint32 `json:"uid"`
		GID uint32 `json:"gid"`
		CWD string `json:"cwd"`
	}
	if err := json.Unmarshal(envelope["payload"], &exec); err != nil || exec.Path != launchctlPath {
		return data
	}
	var jobs []LaunchdJob
	for _, plist := range plistArgs(exec.Args) {
		if !path.IsAbs(plist) {
			if !path.IsAbs(exec.CWD) {
				continue
			}
			plist = strings.TrimSuffix(exec.CWD, "/") + "/" + plist
		}
		if jobs = append(jobs, read(plist, int(exec.UID), int(exec.GID))...); len(jobs) >= maxLaunchdPlists {
			jobs = jobs[:maxLaunchdPlists]
			break
		}
	}
	if len(jobs) == 0 {
		return data
	}
	return encodeEvent(data, envelope, payload, "launchd_jobs", jobs)
}

// plistArgs returns the plist paths a launchctl argv names, at most maxLaunchdPlists:
//
//   - `launchctl load [-wF] [-S sessiontype] [-D domain] paths...`
//   - `launchctl bootstrap <domain-target> paths...`
func plistArgs(args []string) []string {
	if len(args) < 2 {
		return nil
	}
	paths := []string{}
	switch args[1] {
	case "load":
		for i := 2; i < len(args); i++ {
			switch {
			case args[i] == "-S" || args[i] == "-D":
				i++ // the flag's value
			case strings.HasPrefix(args[i], "-"):
			default:
				paths = append(paths, args[i])
			}
		}
	case "bootstrap":
		if len(args) > 3 {
			paths = args[3:]
		}
	}
	return paths[:min(len(paths), maxLaunchdPlists)]
}
