package arch_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

// spec:release-packaging/mysql-s-buffer-pool-is-sized-for-the-session-views/the-quickstart-buffer-pool-defaults-to-1-gb-and-follows-env
//
// TestQuickstartMySQLBufferPoolDefaultsToOneGigabyte pins the quickstart's buffer pool to 1 GB unless .env names another size. At
// MySQL's own 128 MB default the session views read most pages from disk: about 3 s for a long session's process tree on dogfood,
// against 0.05 s from memory (issue #1550).
func TestQuickstartMySQLBufferPoolDefaultsToOneGigabyte(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../docker-compose.quickstart.yml")
	require.NoError(t, err)
	var doc composeDoc
	require.NoError(t, yaml.Unmarshal(raw, &doc))
	pool, ok := commandFlag(doc.Services["mysql"].Command, "--innodb-buffer-pool-size")
	require.True(t, ok, "MySQL is started without a buffer pool size")
	assert.Equal(t, "${EDR_MYSQL_BUFFER_POOL_SIZE:-1G}", pool)
}
