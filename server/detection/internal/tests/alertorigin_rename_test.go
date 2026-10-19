//go:build integration

package tests

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/coordination/leader"
	"github.com/rawtrail/rawtrail/server/detection/bootstrap"
)

// TestRecreditFormerProjectOrigin_RecreditsOnlyThisProjectsAlerts drives the boot-time re-credit through the leader lock, as the
// server runs it, over alerts credited under the old product name beside an upstream credit and a blank pre-attribution row. Only
// the first moves, its triage clock does not, and the pass records itself done so a second start does not walk the table again.
// spec:product-identity/existing-alerts-are-credited-to-rawtrail-on-upgrade/an-upgrade-re-credits-only-this-project-s-alerts
func TestRecreditFormerProjectOrigin_RecreditsOnlyThisProjectsAlerts(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	d := newDetection(t, detectionOpts{mode: bootstrap.ModeFull})
	db := d.Store().DB()

	ours := insertAlertWithOrigin(t, ctx, d, "suspicious_exec", "Fleet EDR", `{"pid":1}`)
	upstream := insertAlertWithOrigin(t, ctx, d, "proc_creation_macos_applescript", "SigmaHQ, by remotephone", `{"pid":2}`)
	blank := insertAlertWithOrigin(t, ctx, d, "suspicious_exec", "", `{"pid":3}`)
	lastTriage := time.Date(2025, 1, 2, 3, 4, 5, 600000000, time.UTC)
	_, err := db.ExecContext(ctx, `UPDATE alerts SET updated_at = ?`, lastTriage)
	require.NoError(t, err)

	coord := leader.NewMySQL(db, slog.New(slog.DiscardHandler))
	ran, err := d.RecreditFormerProjectOrigin(ctx, coord)
	require.NoError(t, err)
	require.True(t, ran, "the only replica takes the lock")

	assert.Equal(t, "RawTrail", originOfAlert(t, ctx, d, ours))
	assert.Equal(t, "SigmaHQ, by remotephone", originOfAlert(t, ctx, d, upstream), "an upstream credit names someone else")
	assert.Empty(t, originOfAlert(t, ctx, d, blank), "a pre-attribution row stays blank")

	var after time.Time
	require.NoError(t, db.GetContext(ctx, &after, `SELECT updated_at FROM alerts WHERE id = ?`, ours))
	assert.True(t, lastTriage.Equal(after), "re-crediting is not triage activity: want %s, got %s", lastTriage, after)

	// A row credited the old way after the pass finished stays as it is: the recorded completion skips the walk entirely.
	late := insertAlertWithOrigin(t, ctx, d, "suspicious_exec", "Fleet EDR", `{"pid":4}`)
	ranAgain, err := d.RecreditFormerProjectOrigin(ctx, coord)
	require.NoError(t, err)
	assert.False(t, ranAgain, "a recorded pass is not taken again")
	assert.Equal(t, "Fleet EDR", originOfAlert(t, ctx, d, late))
}
