package seed_test

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/identity/internal/rbac"
	"github.com/rawtrail/rawtrail/server/identity/internal/seed"
	"github.com/rawtrail/rawtrail/server/identity/internal/users"
	identitymigrations "github.com/rawtrail/rawtrail/server/identity/migrations"
	"github.com/rawtrail/rawtrail/server/migrations/runner"
	"github.com/rawtrail/rawtrail/server/migrations/runner/runnertest"
	"github.com/rawtrail/rawtrail/server/testdb"
)

// renameMigration is the migration that moves the break-glass admin to the RawTrail address.
const renameMigration = "00010_rename_breakglass_email.sql"

// spec:product-identity/the-break-glass-account-moves-to-the-rawtrail-address-on-upgrade/an-upgraded-server-keeps-one-break-glass-account
// TestRenameMigration_UpgradedServerKeepsOneBreakglassAdmin upgrades a database whose break-glass admin was seeded at the Fleet
// EDR address. The account must move to the RawTrail address with its id intact, so the seed finds it rather than creating a
// second super_admin break-glass account and printing a redemption URL for it.
func TestRenameMigration_UpgradedServerKeepsOneBreakglassAdmin(t *testing.T) {
	t.Parallel()
	db := testdb.Open(t)
	opts := runner.Options{Context: "identity", TableName: "identity_goose_db_version"}
	require.NoError(t, runner.Up(t.Context(), db, runnertest.Before(t, identitymigrations.FS, renameMigration), opts))

	res, err := db.ExecContext(t.Context(), `INSERT INTO users (email, is_breakglass) VALUES ('admin@fleet-edr.local', 1)`)
	require.NoError(t, err)
	oldID, err := res.LastInsertId()
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO principals (id, type, display_label) VALUES (CONCAT('usr_', ?), 'user', ?)`,
		oldID, "admin@fleet-edr.local")
	require.NoError(t, err)

	require.NoError(t, runner.Up(t.Context(), db, identitymigrations.FS, opts))
	require.NoError(t, seed.Roles(t.Context(), db), "boot seeds the built-in roles after migrating")

	var label string
	require.NoError(t, db.GetContext(t.Context(), &label, `SELECT display_label FROM principals WHERE id = CONCAT('usr_', ?)`, oldID))
	assert.Equal(t, seed.DefaultAdminEmail, label, "the principal's label follows the email")

	admin, _, err := seed.Admin(t.Context(), users.New(db), rbac.New(db), slog.Default(), nil)
	require.NoError(t, err)
	assert.Equal(t, oldID, admin.ID, "the seed finds the migrated account instead of creating one")
	assert.Equal(t, "admin@rawtrail.local", admin.Email)

	var breakglass int
	require.NoError(t, db.GetContext(t.Context(), &breakglass, `SELECT COUNT(*) FROM users WHERE is_breakglass = 1`))
	assert.Equal(t, 1, breakglass, "exactly one break-glass account after the upgrade")
}
