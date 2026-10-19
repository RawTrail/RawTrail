// Package runnertest helps tests stage a database at an earlier point in a context's migration corpus, so a data migration can be
// exercised against the rows a deployment on the previous release actually holds.
package runnertest

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

// Before returns the migrations in fsys that sort before name, which is the corpus a deployment that has not yet applied name is
// running. Goose orders by the NNNNN version prefix, so a lexical comparison of the zero-padded file names matches its order.
func Before(t *testing.T, fsys fs.FS, name string) fs.FS {
	t.Helper()
	entries, err := fs.ReadDir(fsys, ".")
	require.NoError(t, err)
	before := fstest.MapFS{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") || e.Name() >= name {
			continue
		}
		body, err := fs.ReadFile(fsys, e.Name())
		require.NoError(t, err)
		before[e.Name()] = &fstest.MapFile{Data: body}
	}
	return before
}
