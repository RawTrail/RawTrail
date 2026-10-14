//go:build integration

package tests

import (
	"context"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fleetdm/edr/server/detection/api"
)

const zshSigning = `{"team_id":"","signing_id":"com.apple.zsh","flags":1,"is_platform_binary":true}`

// seedImageRow stores one exec'd process generation directly, so a test can stand the host in a state the builder reaches only through
// events processed out of order.
func seedImageRow(t *testing.T, ctx context.Context, db *sqlx.DB, host string, pid int, pidversion uint32, forkNs, execNs int64,
	path, signing string) {
	t.Helper()
	var cs any
	if signing != "" {
		cs = signing
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO processes (host_id, pid, ppid, path, code_signing, pidversion, fork_time_ns, exec_time_ns)
		VALUES (?, ?, 1, ?, ?, ?, ?, ?)`, host, pid, path, cs, pidversion, forkNs, execNs)
	require.NoError(t, err)
}

// The dev server case from issue #1298. zsh (pid 500, generation 1001) forked a child, but the events arrived out of order: python3,
// which took the pid after zsh exited, was already stored, and zsh's exit was not yet. The child's fork names zsh exactly, so it runs
// zsh's image, where picking by time hands it python3's, the newest generation of the pid to have forked by the child's stamp.
//
// spec:server-process-graph-builder/forked-child-inherits-its-resolved-creator-s-image/a-child-runs-the-image-of-the-creator-it-names
// spec:server-process-graph-builder/forked-child-inherits-its-resolved-creator-s-image/a-child-that-names-no-creator-inherits-as-before
func TestForkInheritsResolvedCreatorImage(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	for _, tc := range []struct {
		name      string
		forkNs    int64
		fork      string
		wantPath  string
		wantSigns string
	}{
		{
			name:     "a child naming its creator runs the creator's image and identity",
			forkNs:   450,
			fork:     `{"child_pid":600,"parent_pid":500,"pidversion":3000,"parent_pidversion":1001}`,
			wantPath: "/bin/zsh", wantSigns: zshSigning,
		},
		{
			name:     "a child naming no creator inherits by time, as before",
			forkNs:   450,
			fork:     `{"child_pid":600,"parent_pid":500}`,
			wantPath: "/usr/bin/python3",
		},
		{
			// Generation 5 matches no stored row, and the resolver's own fallback does not take python3 for it, so no creator is
			// resolved and the child inherits by time.
			name:     "a child naming a generation that does not resolve inherits by time",
			forkNs:   450,
			fork:     `{"child_pid":600,"parent_pid":500,"pidversion":3000,"parent_pidversion":5}`,
			wantPath: "/usr/bin/python3",
		},
		{
			// zsh's image was applied at 110: a child stamped before then cannot have been running it, so the path stays but the
			// identity is not claimed, by the rule the time-based lookup applies too.
			name:     "a creator whose image was applied after the child's stamp gives its path and not its identity",
			forkNs:   105,
			fork:     `{"child_pid":600,"parent_pid":500,"pidversion":3000,"parent_pidversion":1001}`,
			wantPath: "/bin/zsh",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b, _, db := twoBuilders(t)
			host := "creator-image-" + t.Name()
			seedImageRow(t, ctx, db, host, 500, 1001, 100, 110, "/bin/zsh", zshSigning)
			seedImageRow(t, ctx, db, host, 500, 2001, 400, 410, "/usr/bin/python3", "")
			fork := lineageEvt("f-child", "fork", tc.forkNs, tc.fork)
			require.NoError(t, b.ProcessBatch(ctx, rewriteHost([]api.Event{fork}, host)))

			var path string
			var signing *string
			require.NoError(t, db.QueryRowContext(ctx, "SELECT path, code_signing FROM processes WHERE host_id = ? AND pid = 600", host).
				Scan(&path, &signing))
			assert.Equal(t, tc.wantPath, path)
			if tc.wantSigns == "" {
				assert.Nil(t, signing)
			} else {
				require.NotNil(t, signing)
				assert.JSONEq(t, tc.wantSigns, *signing)
			}
		})
	}
}
