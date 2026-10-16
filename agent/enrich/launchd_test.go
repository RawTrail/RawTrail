package enrich

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

const jobPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>com.fleetdm.qa1341.load</string>
<key>EnvironmentVariables</key><dict><key>Label</key><string>not this one</string></dict>
<key>ProgramArguments</key><array><string>/bin/sleep</string><string>71</string></array>
<key>KeepAlive</key><true/>
<key>RunAtLoad</key><true/>
</dict></plist>
`

func execEvent(t *testing.T, payload map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"event_id": "e1", "host_id": "h1", "event_type": "exec", "timestamp_ns": 1, "payload": payload})
	require.NoError(t, err)
	return raw
}

func jobsOf(t *testing.T, data []byte) []LaunchdJob {
	t.Helper()
	var e struct {
		Payload struct {
			Jobs []LaunchdJob `json:"launchd_jobs"`
		} `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(data, &e))
	return e.Payload.Jobs
}

// spec:endpoint-event-collection/launchd-job-definitions/a-launchctl-load-carries-the-jobs-it-names
// fakeReader reads a job named for each plist, except an unreadable one or for another user, recording what it was asked for.
func fakeReader(asked *[]string) JobReader {
	return func(path string, uid, _ int) (LaunchdJob, bool) {
		*asked = append(*asked, path)
		if strings.HasSuffix(path, "unreadable.plist") || uid != 501 {
			return LaunchdJob{}, false
		}
		return LaunchdJob{Label: "job:" + filepath.Base(path)}, true
	}
}

func TestLaunchdJobs(t *testing.T) {
	t.Parallel()
	t.Run("load with flags reads each plist it names, a relative one against the cwd", func(t *testing.T) {
		t.Parallel()
		var asked []string
		read := fakeReader(&asked)
		data := LaunchdJobs(execEvent(t, map[string]any{"pid": 1, "path": "/bin/launchctl", "uid": 501, "cwd": "/Users/v/proj",
			"args": []string{"launchctl", "load", "-w", "-S", "Aqua", "/tmp/a.plist", "b.plist", "unreadable.plist", "dir/../c.plist"}}), read)
		// A relative path is read as given, `..` and all, so a symlinked dir resolves as launchctl's own open did.
		assert.Equal(t, []string{
			"/tmp/a.plist", "/Users/v/proj/b.plist", "/Users/v/proj/unreadable.plist", "/Users/v/proj/dir/../c.plist",
		}, asked)
		assert.Equal(t, []LaunchdJob{
			{PlistPath: "/tmp/a.plist", Label: "job:a.plist"},
			{PlistPath: "/Users/v/proj/b.plist", Label: "job:b.plist"},
			{PlistPath: "/Users/v/proj/dir/../c.plist", Label: "job:c.plist"},
		}, jobsOf(t, data), "the unreadable plist is left out")
	})
	t.Run("bootstrap reads the plists after the domain", func(t *testing.T) {
		t.Parallel()
		var asked []string
		read := fakeReader(&asked)
		data := LaunchdJobs(execEvent(t, map[string]any{"pid": 1, "path": "/bin/launchctl", "uid": 501,
			"args": []string{"launchctl", "bootstrap", "gui/501", "/tmp/c.plist"}}), read)
		assert.Equal(t, []LaunchdJob{{PlistPath: "/tmp/c.plist", Label: "job:c.plist"}}, jobsOf(t, data))
	})
	t.Run("a relative plist without a working directory is skipped", func(t *testing.T) {
		t.Parallel()
		var asked []string
		read := fakeReader(&asked)
		original := execEvent(t, map[string]any{"pid": 1, "path": "/bin/launchctl", "uid": 501, "args": []string{"launchctl", "load", "b.plist"}})
		assert.Equal(t, original, LaunchdJobs(original, read))
	})
	t.Run("at most eight plists are read", func(t *testing.T) {
		t.Parallel()
		var asked []string
		read := fakeReader(&asked)
		args := []string{"launchctl", "load"}
		for i := range 12 {
			args = append(args, filepath.Join("/tmp", string(rune('a'+i))+".plist"))
		}
		data := LaunchdJobs(execEvent(t, map[string]any{"pid": 1, "path": "/bin/launchctl", "uid": 501, "args": args}), read)
		assert.Len(t, jobsOf(t, data), maxLaunchdPlists)
	})
	for name, payload := range map[string]map[string]any{
		"another program":       {"pid": 1, "path": "/usr/bin/open", "uid": 501, "args": []string{"open", "load", "/tmp/a.plist"}},
		"a launchctl elsewhere": {"pid": 1, "path": "/tmp/launchctl", "uid": 501, "args": []string{"launchctl", "load", "/tmp/a.plist"}},
		"another subcommand":    {"pid": 1, "path": "/bin/launchctl", "uid": 501, "args": []string{"launchctl", "kickstart", "gui/501/x"}},
		"an unreadable plist":   {"pid": 1, "path": "/bin/launchctl", "uid": 502, "args": []string{"launchctl", "load", "/tmp/a.plist"}},
		"jobs already sent": {"pid": 1, "path": "/bin/launchctl", "uid": 501, "args": []string{"launchctl", "load", "/tmp/a.plist"},
			"launchd_jobs": []LaunchdJob{{PlistPath: "/x", Label: "from the source"}}},
	} {
		t.Run(name+" passes through untouched", func(t *testing.T) {
			t.Parallel()
			var asked []string
			read := fakeReader(&asked)
			original := execEvent(t, payload)
			assert.Equal(t, original, LaunchdJobs(original, read))
		})
	}
}

// The event loop waits a bounded time for a read, and one read at a time runs: a stuck read cannot hold back the telemetry behind it.
//
// spec:endpoint-event-collection/launchd-job-definitions/a-blocked-plist-read-does-not-hold-back-telemetry
func TestBoundedReader(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	slow := func(path string, _, _ int) (LaunchdJob, bool) {
		if path == "/slow.plist" {
			<-release
		}
		return LaunchdJob{Label: path}, true
	}
	read := BoundedReader(slow, 50*time.Millisecond)

	job, ok := read("/fast.plist", 501, 20)
	require.True(t, ok)
	assert.Equal(t, "/fast.plist", job.Label)

	start := time.Now()
	_, ok = read("/slow.plist", 501, 20)
	assert.False(t, ok, "a read past the wait yields nothing")
	assert.Less(t, time.Since(start), time.Second, "and the caller is not held past the wait")

	start = time.Now()
	_, ok = read("/fast.plist", 501, 20)
	assert.False(t, ok, "while one read is still running, another returns at once")
	assert.Less(t, time.Since(start), 40*time.Millisecond)

	close(release)
	require.Eventually(t, func() bool { _, ok := read("/fast.plist", 501, 20); return ok }, time.Second, 10*time.Millisecond,
		"once the stuck read ends, reads run again")
}

func TestParseLaunchdPlist(t *testing.T) {
	t.Parallel()
	job, ok := parseLaunchdPlist([]byte(jobPlist))
	require.True(t, ok)
	assert.Equal(t, LaunchdJob{Label: "com.fleetdm.qa1341.load", ProgramArguments: []string{"/bin/sleep", "71"}}, job,
		"the top-level keys only: a Label nested in another dictionary is not the job's")

	long := strings.Repeat("é", maxJobFieldBytes)
	many := strings.Repeat("<string>a</string>", maxJobArgs+3)
	capped, ok := parseLaunchdPlist([]byte(`<plist><dict><key>Program</key><string>` + long + `</string>` +
		`<key>ProgramArguments</key><array>` + many + `</array></dict></plist>`))
	require.True(t, ok)
	assert.True(t, capped.Truncated)
	assert.LessOrEqual(t, len(capped.Program), maxJobFieldBytes)
	assert.True(t, strings.HasPrefix(long, capped.Program), "cut on a rune boundary")
	assert.Len(t, capped.ProgramArguments, maxJobArgs)

	for name, doc := range map[string]string{
		"not XML":                 "not a plist",
		"a top-level array":       `<plist><array><string>x</string></array></plist>`,
		"a dict without a plist":  `<dict><key>Label</key><string>x</string></dict>`,
		"no job keys":             `<plist><dict><key>RunAtLoad</key><true/></dict></plist>`,
		"a key without its value": `<plist><dict><key>Label</key></dict></plist>`,
		"unclosed":                `<plist><dict><key>Label</key><string>x`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, ok := parseLaunchdPlist([]byte(doc))
			assert.False(t, ok)
		})
	}
}

// A plist is the user's file, read by a root daemon: no input makes the parser panic or keep more than its bounds.
func FuzzParseLaunchdPlist(f *testing.F) {
	f.Add([]byte(jobPlist))
	f.Add([]byte(`<plist><dict><key>Program</key><string>x</string></dict></plist>`))
	f.Add([]byte(`<plist><dict><key>ProgramArguments</key><array><dict/></array></dict></plist>`))
	f.Fuzz(func(t *testing.T, data []byte) {
		job, ok := parseLaunchdPlist(data)
		if !ok {
			return
		}
		assert.LessOrEqual(t, len(job.Label), maxJobFieldBytes)
		assert.LessOrEqual(t, len(job.Program), maxJobFieldBytes)
		assert.LessOrEqual(t, len(job.ProgramArguments), maxJobArgs)
		for _, a := range job.ProgramArguments {
			assert.LessOrEqual(t, len(a), maxJobArgBytes)
		}
	})
}

// A job definition survives the wire whole: whatever the plist held, Unicode included, decodes back from the event as it was read.
//
// spec:endpoint-event-collection/launchd-job-definitions/a-launchctl-load-carries-the-jobs-it-names
func TestLaunchdJobsRoundTrip(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, maxLaunchdPlists).Draw(rt, "plists")
		want := make([]LaunchdJob, n)
		args := []string{"launchctl", "load"}
		for i := range n {
			path := "/tmp/" + rapid.StringMatching(`[a-z]{1,12}`).Draw(rt, "name") + ".plist"
			args = append(args, path)
			want[i] = LaunchdJob{PlistPath: path, Label: rapid.String().Draw(rt, "label"), Program: rapid.String().Draw(rt, "program"),
				ProgramArguments: rapid.SliceOfN(rapid.String(), 1, 8).Draw(rt, "args"), Truncated: rapid.Bool().Draw(rt, "truncated")}
		}
		next := 0
		read := func(string, int, int) (LaunchdJob, bool) {
			job := want[next]
			job.PlistPath = ""
			next++
			return job, true
		}
		data := LaunchdJobs(execEvent(t, map[string]any{"pid": 1, "path": "/bin/launchctl", "uid": 501, "args": args}), read)
		got := jobsOf(t, data)
		if len(got) != len(want) {
			rt.Fatalf("got %d jobs, want %d", len(got), len(want))
		}
		for i := range want {
			assert.Equal(rt, want[i], got[i])
		}
	})
}
