package graph

import (
	"encoding/json"
	"testing"

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

// spec:server-rest-api/inferred-launch-links/a-launcher-is-matched-to-what-it-named
func TestLaunchTarget(t *testing.T) {
	t.Parallel()
	const textEdit = "/System/Applications/TextEdit.app/Contents/MacOS/TextEdit"
	for _, tc := range []struct {
		name     string
		launcher api.Process
		matches  []string
		misses   []string
	}{
		{
			name:     "open -a by name, any case",
			launcher: launcher(t, "/usr/bin/open", "open", "-a", "textedit"),
			matches:  []string{textEdit},
			misses:   []string{"/Applications/TextEditor.app/Contents/MacOS/TextEditor", "/usr/bin/textedit"},
		},
		{
			name:     "open -a by bundle path",
			launcher: launcher(t, "/usr/bin/open", "open", "-a", "/Applications/Foo Bar.app/"),
			matches:  []string{"/Applications/Foo Bar.app/Contents/MacOS/Foo Bar"},
			misses:   []string{"/Users/v/Applications/Foo Bar.app/Contents/MacOS/Foo Bar"},
		},
		{
			name:     "open of a bundle path",
			launcher: launcher(t, "/usr/bin/open", "open", "-n", "/Applications/Calculator.app"),
			matches:  []string{"/Applications/Calculator.app/Contents/MacOS/Calculator"},
		},
		{
			name:     "launchctl submit of an absolute program",
			launcher: launcher(t, "/bin/launchctl", "launchctl", "submit", "-l", "com.example.job", "--", "/bin/sleep", "61"),
			matches:  []string{"/bin/sleep"},
			misses:   []string{"/usr/bin/sleep", "/usr/libexec/xpcproxy"},
		},
		{
			name:     "launchctl submit of a bare program",
			launcher: launcher(t, "/bin/launchctl", "launchctl", "submit", "-l", "j", "--", "sleep"),
			matches:  []string{"/bin/sleep", "/usr/bin/sleep"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			match, ok := launchTarget(tc.launcher)
			require.True(t, ok)
			for _, exe := range tc.matches {
				assert.True(t, match(exe), exe)
			}
			for _, exe := range tc.misses {
				assert.False(t, match(exe), exe)
			}
		})
	}
}

// spec:server-rest-api/inferred-launch-links/a-launcher-is-matched-to-what-it-named
func TestLaunchTarget_NamesNothingMatchable(t *testing.T) {
	t.Parallel()
	for name, p := range map[string]api.Process{
		"open by bundle id":           launcher(t, "/usr/bin/open", "open", "-b", "com.apple.TextEdit"),
		"open of a document":          launcher(t, "/usr/bin/open", "open", "notes.txt"),
		"open with no target":         launcher(t, "/usr/bin/open", "open"),
		"launchctl load of a plist":   launcher(t, "/bin/launchctl", "launchctl", "load", "/Library/LaunchAgents/x.plist"),
		"launchctl submit without --": launcher(t, "/bin/launchctl", "launchctl", "submit", "-l", "j"),
		"launchctl submit ending --":  launcher(t, "/bin/launchctl", "launchctl", "submit", "-l", "j", "--"),
		"not a launcher":              launcher(t, "/bin/sleep", "sleep", "61"),
		"another tool with submit":    launcher(t, "/usr/local/bin/jobs", "jobs", "submit", "-l", "j", "--", "/bin/sleep"),
		"a launchctl elsewhere":       launcher(t, "/tmp/launchctl", "launchctl", "submit", "-l", "j", "--", "/bin/sleep"),
		"an open elsewhere":           launcher(t, "/opt/bin/open", "open", "-a", "TextEdit"),
		"an argv that is not JSON":    {Path: "/usr/bin/open", Args: api.NullRawJSON("not json")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, ok := launchTarget(p)
			assert.False(t, ok)
		})
	}
}
