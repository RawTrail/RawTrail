//go:build integration

package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// The archive reports the space its own database's tables take, and that space grows as events are written (issue #1434). Each test
// archive is its own database, so the figure is this test's events alone.
// spec:server-configuration/the-store-size-is-reported/the-store-size-and-retention-are-reported
func TestEventArchive_ArchiveBytesCountsItsOwnTables(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	arch := openTestArchive(t)

	empty, err := arch.ArchiveBytes(ctx)
	require.NoError(t, err)

	nowNs := time.Now().UnixNano()
	events := make([]visibilityapi.Event, 0, 200)
	for i := range 200 {
		events = append(events, archiveEvent(fmt.Sprintf("bytes-%d", i), "hbytes", "network_connect",
			nowNs+int64(i), 4242))
	}
	require.NoError(t, arch.Insert(ctx, events))

	written, err := arch.ArchiveBytes(ctx)
	require.NoError(t, err)
	assert.Greater(t, written, empty, "written events take space on disk")
}
