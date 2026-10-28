package sessioncommands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

// spec:server-rest-api/agent-session-commands/a-query-keeps-the-commands-it-finds
func TestSearch(t *testing.T) {
	t.Parallel()
	commands := Build(realSession())
	for _, tc := range []struct {
		name, query string
		want        []string
	}{
		{"the command the agent ran", "QA1280", []string{"env -i /bin/echo qa1280", `/bin/sh -c "sleep 1; echo qa1280-done"`}},
		{"a process's path", "/usr/bin/head", []string{"ls /private/tmp | head -3"}},
		{"a process's command line", "sleep 47", []string{"(sleep 47 &)"}},
		{"a process whose text the command does not hold", "/bin/bash", []string{`/bin/sh -c "sleep 1; echo qa1280-done"`}},
		// The wrapper's boilerplate is in every call's script; finding it would find every command.
		{"not the wrapper shell's script", "shell-snapshots", []string{}},
		{"the wrapper shell's path", "/bin/zsh", []string{
			"ls /private/tmp | head -3", "(sleep 47 &)", "nohup sleep 48 >/dev/null 2>&1 &", "env -i /bin/echo qa1280",
			`/bin/sh -c "sleep 1; echo qa1280-done"`,
		}},
		{"nothing", "no-such-thing", []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, labels(Search(commands, tc.query)))
		})
	}
	t.Run("an empty query keeps every command", func(t *testing.T) {
		t.Parallel()
		assert.Len(t, Search(commands, "  "), 5)
	})
	t.Run("the root of a command the agent did not run is searched", func(t *testing.T) {
		t.Parallel()
		hook := Build([]api.ProcessNode{proc(7, 7, 50, "/usr/bin/osascript", []string{"osascript", "-e", "beep"})})
		assert.Len(t, Search(hook, "osascript"), 1)
	})
}

// spec:server-rest-api/agent-session-commands/commands-page-in-the-order-they-started
func TestPageOf(t *testing.T) {
	t.Parallel()
	commands := Build(realSession())
	first, err := PageOf(commands, "", 2)
	require.NoError(t, err)
	assert.Equal(t, []string{"ls /private/tmp | head -3", "(sleep 47 &)"}, labels(first.Commands))
	assert.Equal(t, 5, first.TotalMatched)
	require.NotEmpty(t, first.NextCursor)
	second, err := PageOf(commands, first.NextCursor, 2)
	require.NoError(t, err)
	assert.Equal(t, []string{"nohup sleep 48 >/dev/null 2>&1 &", "env -i /bin/echo qa1280"}, labels(second.Commands))
	last, err := PageOf(commands, second.NextCursor, 2)
	require.NoError(t, err)
	assert.Len(t, last.Commands, 1)
	assert.Empty(t, last.NextCursor, "the last page names no next")

	exact, err := PageOf(commands, "", 5)
	require.NoError(t, err)
	assert.Len(t, exact.Commands, 5)
	assert.Empty(t, exact.NextCursor, "a page that ends on the last command names no next")

	empty, err := PageOf([]Command{}, "", 50)
	require.NoError(t, err)
	assert.Equal(t, Page{Commands: []Command{}}, empty)

	for _, token := range []string{"!!", "bm90LWpzb24", "e30", "eyJzIjoxfQ"} { // not base64url, not JSON, {}, {"s":1}
		_, err := PageOf(commands, token, 2)
		assert.ErrorIs(t, err, api.ErrInvalidCursor, token)
	}
}

// Reading every page in turn lists every command once, in order, whatever the page size, and commands the live session starts
// between reads, after the ones already read, are listed after them.
//
// spec:server-rest-api/agent-session-commands/commands-page-in-the-order-they-started
func TestPageOf_Property(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 30).Draw(t, "commands")
		var roots []api.ProcessNode
		add := func(id int64, at int64) {
			pid := 1000 + int(id)
			roots = append(roots, keyed(proc(id, pid, at, "/bin/zsh", []string{"/bin/zsh", "-c", "eval 'ls'"}), pid))
		}
		for i := range n {
			// Shared start times, as calls of one turn can have, are ordered by row.
			add(int64(i+1), int64(rapid.IntRange(1, 10).Draw(t, "at")))
		}
		limit := rapid.IntRange(1, 7).Draw(t, "limit")
		growth := rapid.IntRange(0, 5).Draw(t, "growth")
		var read []string
		token := ""
		nextID, nextAt := int64(n+1), int64(11)
		for range n + growth + 1 {
			page, err := PageOf(Build(roots), token, limit)
			require.NoError(t, err)
			for _, c := range page.Commands {
				read = append(read, c.Key)
			}
			if page.NextCursor == "" {
				break
			}
			token = page.NextCursor
			// The session runs on while the client pages it: a command starts after every one so far, behind the cursor.
			if growth > 0 {
				add(nextID, nextAt)
				nextID, nextAt, growth = nextID+1, nextAt+1, growth-1
			}
		}
		var want []string
		for _, c := range Build(roots) {
			want = append(want, c.Key)
		}
		require.Equal(t, want, read)
	})
}
