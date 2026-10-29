// Package agentsettings stores the agent settings and pushes them to hosts (issue #1426): the fleet-wide switches for what the agent
// collects. They reach hosts the way the watched-path set does (hostpush); what is here is their storage, payload and audit row.
package agentsettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/rawtrail/rawtrail/internal/versionedset"
	"github.com/rawtrail/rawtrail/server/auditoutbox"
	"github.com/rawtrail/rawtrail/server/rules/api"
	"github.com/rawtrail/rawtrail/server/rules/internal/detectionconfig"
	"github.com/rawtrail/rawtrail/server/rules/internal/hostpush"
)

// Store reads and replaces the single agent_settings row. Its concurrency is versionedset's.
type Store struct {
	set    *versionedset.Store
	outbox *auditoutbox.Store
}

// NewStore builds a Store. Panics on a nil db, which is a wiring bug.
func NewStore(db *sqlx.DB) *Store {
	if db == nil {
		panic("agentsettings.NewStore: db must not be nil")
	}
	return &Store{
		set:    versionedset.New(db, versionedset.Table{Name: "agent_settings", PayloadColumn: "settings", Noun: "agent settings"}),
		outbox: auditoutbox.NewStore(db, detectionconfig.AuditOutboxTable),
	}
}

// ErrVersionConflict is returned for a conditional replacement of settings that have changed since the caller read them.
var ErrVersionConflict = errors.New("the agent settings were changed since they were read")

// stored is the settings column. A switch is a pointer so a key that is absent, as in the seeded row, reads as its default.
type stored struct {
	CapturePrompts *bool `json:"capture_prompts,omitempty"`
}

// decode maps a stored row onto the API's settings, defaults filled in.
func decode(row versionedset.Row) (api.AgentSettings, error) {
	var s stored
	if err := json.Unmarshal(row.Payload, &s); err != nil {
		return api.AgentSettings{}, fmt.Errorf("decode agent settings: %w", err)
	}
	out := api.DefaultAgentSettings()
	out.Version, out.UpdatedBy, out.UpdatedAt = row.Version, row.UpdatedBy, row.UpdatedAtPtr()
	if s.CapturePrompts != nil {
		out.CapturePrompts = *s.CapturePrompts
	}
	return out, nil
}

// Get returns the stored settings.
func (s *Store) Get(ctx context.Context) (api.AgentSettings, error) {
	row, err := s.set.Get(ctx)
	if err != nil {
		return api.AgentSettings{}, err
	}
	return decode(row)
}

// Replace stores capturePrompts as the new settings and returns the settings it replaced and the new ones, one version past them.
//
// A non-nil expectedVersion makes the replacement conditional: when the stored settings are at any other version, Replace stores
// nothing and returns ErrVersionConflict.
//
// The audit entry audit builds from the two is written in the same transaction, held for auditHold so the caller can add the push's
// host counts with SealAudit before it is delivered. Replace returns the entry's id for that.
func (s *Store) Replace(
	ctx context.Context, capturePrompts bool, actor string, expectedVersion *int64,
	audit func(previous, next api.AgentSettings) (auditoutbox.Entry, error),
) (previous, next api.AgentSettings, auditID int64, err error) {
	// Marshalling a struct of one boolean cannot fail.
	encoded, _ := json.Marshal(stored{CapturePrompts: &capturePrompts})
	return hostpush.ReplaceAudited(ctx, s.set, s.outbox, encoded, actor, expectedVersion, ErrVersionConflict, auditHold, decode, audit)
}

// auditHold is how long a replacement's audit entry waits for the push's host counts, as for the watched-path set: the push takes well
// under a second, so the hold only matters when the replica died before adding the counts.
const auditHold = 5 * time.Minute

// SealAudit adds the push's host counts to a replacement's held audit entry and releases it for delivery. It reports false when the
// hold had already passed, in which case the entry is delivered without the counts.
func (s *Store) SealAudit(ctx context.Context, id int64, entry auditoutbox.Entry) (bool, error) {
	return s.outbox.Seal(ctx, id, entry)
}
