//go:build integration

package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	detectionmigrations "github.com/rawtrail/rawtrail/server/detection/migrations"
	"github.com/rawtrail/rawtrail/server/migrations/runner"
	"github.com/rawtrail/rawtrail/server/migrations/runner/runnertest"
	"github.com/rawtrail/rawtrail/server/testdb"
)

// TestAlertOriginRename_RecreditsOnlyThisProjectsAlerts upgrades a database holding alerts credited under the old product name
// beside an upstream credit and a blank pre-attribution row. Only the first moves: the others name someone else, or nobody.
// spec:product-identity/existing-alerts-are-credited-to-rawtrail-on-upgrade/an-upgrade-re-credits-only-this-project-s-alerts
func TestAlertOriginRename_RecreditsOnlyThisProjectsAlerts(t *testing.T) {
	t.Parallel()
	db := testdb.Open(t)
	opts := runner.Options{Context: "detection", TableName: "detection_goose_db_version"}
	require.NoError(t, runner.Up(t.Context(), db, runnertest.Before(t, detectionmigrations.FS, "00023_alerts_origin_rawtrail.sql"), opts))

	seeded := map[string]string{"ours": "Fleet EDR", "upstream": "SigmaHQ, by remotephone", "blank": ""}
	for subject, origin := range seeded {
		_, err := db.ExecContext(t.Context(),
			`INSERT INTO alerts (host_id, rule_id, source, severity, title, description, origin, subject, techniques)
			 VALUES ('host-1366', 'suspicious_exec', 'detection', 'high', 'seeded', 'seeded', ?, ?, '[]')`, origin, subject)
		require.NoError(t, err)
	}

	require.NoError(t, runner.Up(t.Context(), db, detectionmigrations.FS, opts))

	want := map[string]string{"ours": "RawTrail", "upstream": "SigmaHQ, by remotephone", "blank": ""}
	for subject, origin := range want {
		var got string
		require.NoError(t, db.GetContext(t.Context(), &got, `SELECT origin FROM alerts WHERE subject = ?`, subject))
		assert.Equal(t, origin, got, subject)
	}
}
