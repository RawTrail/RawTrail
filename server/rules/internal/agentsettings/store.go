// Package agentsettings stores the agent settings and pushes them to hosts (issue #1426): the fleet-wide switches for what the agent
// collects. They reach hosts the way the watched-path set does (hostpush); what is here is their storage, payload and audit row.
package agentsettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"

	"github.com/rawtrail/rawtrail/internal/versionedset"
	"github.com/rawtrail/rawtrail/server/auditoutbox"
	"github.com/rawtrail/rawtrail/server/rules/api"
	"github.com/rawtrail/rawtrail/server/rules/internal/detectionconfig"
	"github.com/rawtrail/rawtrail/server/rules/internal/hostpush"
)

// Store reads and replaces the single agent_settings row. Its concurrency is versionedset's.
type Store struct {
	*hostpush.Document[api.AgentSettings]
}

// NewStore builds a Store. Panics on a nil db, which is a wiring bug.
func NewStore(db *sqlx.DB) *Store {
	table := versionedset.Table{Name: "agent_settings", PayloadColumn: "settings", Noun: "agent settings"}
	return &Store{hostpush.NewDocument(db, table, detectionconfig.AuditOutboxTable, decode, ErrVersionConflict)}
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

// Replace stores capturePrompts as the new settings and returns the settings it replaced and the new ones, one version past them.
//
// A non-nil expectedVersion makes the replacement conditional: when the stored settings are at any other version, Replace stores
// nothing and returns ErrVersionConflict.
//
// The audit entry audit builds from the two is written in the same transaction, held for hostpush.AuditHold so the caller can add the push's
// host counts with SealAudit before it is delivered. Replace returns the entry's id for that.
func (s *Store) Replace(
	ctx context.Context, capturePrompts bool, actor string, expectedVersion *int64,
	audit func(previous, next api.AgentSettings) (auditoutbox.Entry, error),
) (previous, next api.AgentSettings, auditID int64, err error) {
	// Marshalling a struct of one boolean cannot fail.
	encoded, _ := json.Marshal(stored{CapturePrompts: &capturePrompts})
	return s.ReplaceEncoded(ctx, encoded, actor, expectedVersion, audit)
}
