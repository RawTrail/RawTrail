package graph

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fleetdm/edr/server/detection/api"
)

func launcher(t *testing.T, exe string, args ...string) api.Process {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	return api.Process{Path: exe, Args: raw}
}

// exe is a launchd child running the executable at path.
func exe(path string) api.Process { return api.Process{Path: path} }

// xpc is launchd's xpcproxy row for the job with the given label.
func xpc(t *testing.T, label string) api.Process {
	t.Helper()
	return launcher(t, "/usr/libexec/xpcproxy", "xpcproxy", label)
}

// signed is a launchd child running path, signed with the given identifier.
func signed(path, id string) api.Process {
	return api.Process{Path: path, CodeSigning: api.NullRawJSON(`{"signing_id":"` + id + `","team_id":""}`)}
}

// spec:server-rest-api/inferred-launch-links/a-launcher-is-matched-to-what-it-named
// spec:server-rest-api/launches-by-label-and-bundle-id/a-label-or-bundle-id-names-its-launch-exactly
func TestLaunchTarget(t *testing.T) {
	t.Parallel()
	const textEdit = "/System/Applications/TextEdit.app/Contents/MacOS/TextEdit"
	for _, tc := range []struct {
		name     string
		launcher api.Process
		matches  []api.Process
		misses   []api.Process
		window   time.Duration
	}{
		{
			name:     "open -a by name, any case",
			launcher: launcher(t, "/usr/bin/open", "open", "-a", "textedit"),
			matches:  []api.Process{exe(textEdit)},
			misses:   []api.Process{exe("/Applications/TextEditor.app/Contents/MacOS/TextEditor"), exe("/usr/bin/textedit")},
			window:   launchWindow,
		},
		{
			name:     "open -a by bundle path",
			launcher: launcher(t, "/usr/bin/open", "open", "-a", "/Applications/Foo Bar.app/"),
			matches:  []api.Process{exe("/Applications/Foo Bar.app/Contents/MacOS/Foo Bar")},
			misses:   []api.Process{exe("/Users/v/Applications/Foo Bar.app/Contents/MacOS/Foo Bar")},
			window:   launchWindow,
		},
		{
			name:     "open of a bundle path",
			launcher: launcher(t, "/usr/bin/open", "open", "-n", "/Applications/Calculator.app"),
			matches:  []api.Process{exe("/Applications/Calculator.app/Contents/MacOS/Calculator")},
			window:   launchWindow,
		},
		{
			name:     "open -b by bundle id, any case",
			launcher: launcher(t, "/usr/bin/open", "open", "-b", "com.apple.TextEdit"),
			matches:  []api.Process{signed(textEdit, "com.apple.textedit")},
			misses:   []api.Process{signed(textEdit, "com.apple.TextEditor"), exe(textEdit)},
			window:   exactLaunchWindow,
		},
		{
			name:     "launchctl submit of an absolute program",
			launcher: launcher(t, "/bin/launchctl", "launchctl", "submit", "-l", "com.example.job", "--", "/bin/sleep", "61"),
			matches:  []api.Process{exe("/bin/sleep")},
			misses:   []api.Process{exe("/usr/bin/sleep"), exe("/usr/libexec/xpcproxy")},
			window:   launchWindow,
		},
		{
			name:     "launchctl submit of a bare program",
			launcher: launcher(t, "/bin/launchctl", "launchctl", "submit", "-l", "j", "--", "sleep"),
			matches:  []api.Process{exe("/bin/sleep"), exe("/usr/bin/sleep")},
			window:   launchWindow,
		},
		{
			name:     "launchctl kickstart -k of a gui job",
			launcher: launcher(t, "/bin/launchctl", "launchctl", "kickstart", "-k", "gui/501/com.fleetdm.qa1341.bootstrap"),
			matches:  []api.Process{xpc(t, "com.fleetdm.qa1341.bootstrap")},
			misses:   []api.Process{xpc(t, "com.fleetdm.qa1341.load"), exe("/bin/sleep")},
			window:   exactLaunchWindow,
		},
		{
			name:     "launchctl kickstart of a system job",
			launcher: launcher(t, "/bin/launchctl", "launchctl", "kickstart", "system/com.example.d"),
			matches:  []api.Process{xpc(t, "com.example.d")},
			window:   exactLaunchWindow,
		},
		{
			name:     "launchctl start by label",
			launcher: launcher(t, "/bin/launchctl", "launchctl", "start", "com.example.job"),
			matches:  []api.Process{xpc(t, "com.example.job")},
			misses:   []api.Process{xpc(t, "com.example.job2")},
			window:   exactLaunchWindow,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			target, ok := launchTarget(tc.launcher)
			require.True(t, ok)
			assert.Equal(t, tc.window, target.window)
			for _, c := range tc.matches {
				assert.True(t, target.match(c), c.Path)
			}
			for _, c := range tc.misses {
				assert.False(t, target.match(c), c.Path)
			}
		})
	}
}

// spec:server-rest-api/inferred-launch-links/a-launcher-is-matched-to-what-it-named
func TestLaunchTarget_NamesNothingMatchable(t *testing.T) {
	t.Parallel()
	for name, p := range map[string]api.Process{
		"launchctl kickstart of no job": launcher(t, "/bin/launchctl", "launchctl", "kickstart", "-k"),
		"launchctl start of no label":   launcher(t, "/bin/launchctl", "launchctl", "start"),
		"open of a document":            launcher(t, "/usr/bin/open", "open", "notes.txt"),
		"open with no target":           launcher(t, "/usr/bin/open", "open"),
		"launchctl load of a plist":     launcher(t, "/bin/launchctl", "launchctl", "load", "/Library/LaunchAgents/x.plist"),
		"launchctl submit without --":   launcher(t, "/bin/launchctl", "launchctl", "submit", "-l", "j"),
		"launchctl submit ending --":    launcher(t, "/bin/launchctl", "launchctl", "submit", "-l", "j", "--"),
		"not a launcher":                launcher(t, "/bin/sleep", "sleep", "61"),
		"another tool with submit":      launcher(t, "/usr/local/bin/jobs", "jobs", "submit", "-l", "j", "--", "/bin/sleep"),
		"a launchctl elsewhere":         launcher(t, "/tmp/launchctl", "launchctl", "submit", "-l", "j", "--", "/bin/sleep"),
		"an open elsewhere":             launcher(t, "/opt/bin/open", "open", "-a", "TextEdit"),
		"an argv that is not JSON":      {Path: "/usr/bin/open", Args: api.NullRawJSON("not json")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, ok := launchTarget(p)
			assert.False(t, ok)
		})
	}
}
