//go:build integration

package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/internal/mysql"
	detectiontestkit "github.com/rawtrail/rawtrail/server/detection/testkit"
	"github.com/rawtrail/rawtrail/server/testdb/full"
)

// The storage report reads the database's own tables from information_schema, and the archive's size from the archive (issue #1434).
// spec:server-configuration/the-store-size-is-reported/the-store-size-and-retention-are-reported
func TestStoreSizes_ReadsTheDatabaseAndTheArchive(t *testing.T) {
	t.Parallel()
	db := full.Open(t)
	archive := detectiontestkit.NewMemArchive()
	require.NoError(t, archive.Insert(t.Context(), []api.Event{{
		EventID: "sizes-1", HostID: "hsizes", TimestampNs: 1, EventType: "exec", Payload: []byte(`{"pid":1,"path":"/bin/ls"}`),
	}}))
	store, err := mysql.New(db, archive, nil)
	require.NoError(t, err)

	sizes, err := store.StoreSizes(t.Context())
	require.NoError(t, err)
	assert.Positive(t, sizes.DatabaseBytes, "a migrated database's tables take space")
	assert.Equal(t, int64(len(`{"pid":1,"path":"/bin/ls"}`)), sizes.EventArchiveBytes)
}
