//go:build integration

package tests

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/httpserver"
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
	chstore "github.com/rawtrail/rawtrail/server/visibility/internal/clickhouse"
)

// The configured retention, not the migration's 30 days, decides which events expire (issue #1434). The 10-day-old event is inside
// the migration's window, so it surviving here would mean the setting never reached the table.
// spec:server-configuration/event-retention-follows-a-setting/the-setting-governs-which-events-expire
func TestEventArchive_RetentionFollowsTheSetting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	arch, chDB := openTestArchiveWithRetention(t, 7)

	const (
		dayNs     = int64(24 * time.Hour)
		host      = "hsetting"
		pid       = 56
		keptID    = "retention-three-days"
		expiredID = "retention-ten-days"
	)
	nowNs := time.Now().UnixNano()
	kept := archiveEvent(keptID, host, "network_connect", nowNs-3*dayNs, pid)
	kept.IngestedAtNs = nowNs - 3*dayNs
	expired := archiveEvent(expiredID, host, "network_connect", nowNs-10*dayNs, pid)
	expired.IngestedAtNs = nowNs - 10*dayNs
	require.NoError(t, arch.Insert(ctx, []visibilityapi.Event{kept, expired}))

	_, err := chDB.ExecContext(ctx, "OPTIMIZE TABLE events FINAL")
	require.NoError(t, err)

	window := httpserver.TimeRange{FromNs: 0, ToNs: nowNs + 1_000_000}
	var after []visibilityapi.Event
	require.Eventually(t, func() bool {
		after, err = arch.NetworkEventsForProcess(ctx, host, pid, window)
		return err == nil && len(after) == 1
	}, 5*time.Second, 100*time.Millisecond, "the 7-day TTL leaves exactly the 3-day-old event")
	assert.Equal(t, []string{keptID}, eventIDs(after))
}

// Applying the retention the table already holds must not ALTER it, so a restart does not rewrite the TTL; a different value must.
// spec:server-configuration/event-retention-follows-a-setting/an-unchanged-setting-does-not-alter-the-table
func TestApplyEventRetention_AltersOnlyOnChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, chDB := openTestArchiveWithRetention(t, 7)

	changed, err := chstore.ApplyEventRetention(ctx, chDB, 7)
	require.NoError(t, err)
	assert.False(t, changed, "startup already set 7 days, so the same value issues no ALTER")

	changed, err = chstore.ApplyEventRetention(ctx, chDB, 30)
	require.NoError(t, err)
	assert.True(t, changed, "a different value is applied")

	var engine string
	require.NoError(t, chDB.GetContext(ctx, &engine,
		"SELECT engine_full FROM system.tables WHERE database = currentDatabase() AND name = 'events'"))
	assert.Contains(t, engine, "TTL ingested_date + toIntervalDay(30)", "the table carries the new TTL")

	changed, err = chstore.ApplyEventRetention(ctx, chDB, 30)
	require.NoError(t, err)
	assert.False(t, changed, "the value just applied is recognized, so the read of the TTL matches what the ALTER wrote")
}
