package sessioncommands

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

func searchProc(id, forkNs int64, path string, argv []string, children ...api.ProcessNode) api.ProcessNode {
	raw, _ := json.Marshal(argv)
	return api.ProcessNode{Process: api.Process{ID: id, Path: path, Args: raw, ForkTimeNs: forkNs}, Children: children}
}

func ids(procs []api.ProcessNode) []int64 {
	out := make([]int64, 0, len(procs))
	for _, p := range procs {
		out = append(out, p.ID)
	}
	return out
}

// spec:server-rest-api/agent-session-process-search/a-search-finds-a-process-on-any-page-of-the-session
func TestSearchProcesses(t *testing.T) {
	t.Parallel()
	// Two commands; the deepest match sits under a later root but forked first, so fork order is not walk order.
	forest := []api.ProcessNode{
		searchProc(1, 100, "/bin/zsh", []string{"zsh", "-c", "make test"},
			searchProc(2, 110, "/usr/bin/make", []string{"make", "test"},
				searchProc(3, 120, "/usr/local/bin/go", []string{"go", "test", "./..."}))),
		searchProc(4, 50, "/bin/zsh", []string{"zsh", "-c", "git status"},
			searchProc(5, 60, "/usr/bin/git", []string{"git", "status"})),
	}
	for _, tc := range []struct {
		name  string
		query string
		want  []int64
	}{
		{"by path, regardless of case", "/USR/BIN/GIT", []int64{5}},
		{"by command line, at any depth", "go test", []int64{3}},
		{"in fork order across commands", "test", []int64{1, 2, 3}},
		{"every process for an empty query, in fork order", "  ", []int64{4, 5, 1, 2, 3}},
		{"nothing for a query nothing holds", "docker", []int64{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			found := SearchProcesses(forest, tc.query)
			assert.Equal(t, tc.want, ids(found))
			for _, p := range found {
				assert.Nil(t, p.Children, "each match is listed alone")
			}
		})
	}
	assert.Len(t, forest[0].Children, 1, "the forest is not changed")
}

// Following ProcessPageOf from the zero position to the end reads every process once, in order, whatever the page size and however
// many share a fork time.
//
// spec:server-rest-api/agent-session-process-search/a-search-pages-by-cursor
func TestProcessPageOf_ReadsEveryProcessOnce(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		forks := rapid.SliceOf(rapid.Int64Range(0, 4)).Draw(rt, "forks")
		var forest []api.ProcessNode
		for i, fork := range forks {
			forest = append(forest, searchProc(int64(i+1), fork, "/bin/sh", []string{"sh"}))
		}
		procs := SearchProcesses(forest, "")
		limit := rapid.IntRange(1, 5).Draw(rt, "limit")
		read := []int64{}
		var after api.TreePosition
		for range len(procs) + 1 {
			page, next := ProcessPageOf(procs, after, limit)
			require.LessOrEqual(rt, len(page), limit)
			// A cursor leads to a page with something on it: only a search that found nothing has an empty page.
			require.True(rt, len(page) > 0 || len(procs) == 0, "an empty page after a cursor")
			read = append(read, ids(page)...)
			if next == nil {
				break
			}
			after = *next
		}
		require.Equal(rt, ids(procs), read)
	})
}
