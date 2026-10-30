// Package credentialguard stores the credential guard's policy and pushes it to hosts (issue #1432): the guard's mode and the
// credential files it protects. It reaches hosts the way the agent settings do (hostpush); what is here is its storage, payload and
// audit row.
package credentialguard

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

// Store reads and replaces the single credential_guard row. Its concurrency is versionedset's.
type Store struct {
	set    *versionedset.Store
	outbox *auditoutbox.Store
}

// NewStore builds a Store. Panics on a nil db, which is a wiring bug.
func NewStore(db *sqlx.DB) *Store {
	if db == nil {
		panic("credentialguard.NewStore: db must not be nil")
	}
	return &Store{
		set:    versionedset.New(db, versionedset.Table{Name: "credential_guard", PayloadColumn: "policy", Noun: "credential guard policy"}),
		outbox: auditoutbox.NewStore(db, detectionconfig.AuditOutboxTable),
	}
}

// ErrVersionConflict is returned for a conditional replacement of a policy that has changed since the caller read it.
var ErrVersionConflict = errors.New("the credential guard policy was changed since it was read")

// stored is the policy column. Mode is empty in the seeded row, which reads as the default policy.
type stored struct {
	Mode        string                    `json:"mode,omitempty"`
	Credentials []api.ProtectedCredential `json:"credentials,omitempty"`
}

// decode maps a stored row onto the API's policy, the default one for a row no operator has changed.
func decode(row versionedset.Row) (api.CredentialGuardPolicy, error) {
	var s stored
	if err := json.Unmarshal(row.Payload, &s); err != nil {
		return api.CredentialGuardPolicy{}, fmt.Errorf("decode credential guard policy: %w", err)
	}
	out := api.DefaultCredentialGuard()
	if s.Mode != "" {
		out.Mode = s.Mode
		out.Credentials = s.Credentials
		if out.Credentials == nil {
			out.Credentials = []api.ProtectedCredential{}
		}
	}
	out.Version, out.UpdatedBy, out.UpdatedAt = row.Version, row.UpdatedBy, row.UpdatedAtPtr()
	return out, nil
}

// Get returns the stored policy.
func (s *Store) Get(ctx context.Context) (api.CredentialGuardPolicy, error) {
	row, err := s.set.Get(ctx)
	if err != nil {
		return api.CredentialGuardPolicy{}, err
	}
	return decode(row)
}

// Replace stores mode and credentials as the new policy and returns the policy it replaced and the new one, one version past it.
// A non-nil expectedVersion makes the replacement conditional, as for the agent settings, and the audit entry audit builds is written
// in the same transaction, held for auditHold so the caller can add the push's host counts with SealAudit.
func (s *Store) Replace(
	ctx context.Context, mode string, credentials []api.ProtectedCredential, actor string, expectedVersion *int64,
	audit func(previous, next api.CredentialGuardPolicy) (auditoutbox.Entry, error),
) (previous, next api.CredentialGuardPolicy, auditID int64, err error) {
	if credentials == nil {
		credentials = []api.ProtectedCredential{}
	}
	encoded, err := json.Marshal(stored{Mode: mode, Credentials: credentials})
	if err != nil {
		return api.CredentialGuardPolicy{}, api.CredentialGuardPolicy{}, 0, fmt.Errorf("encode credential guard policy: %w", err)
	}
	return hostpush.ReplaceAudited(ctx, s.set, s.outbox, encoded, actor, expectedVersion, ErrVersionConflict, auditHold, decode, audit)
}

// auditHold is how long a replacement's audit entry waits for the push's host counts, as for the agent settings.
const auditHold = 5 * time.Minute

// SealAudit adds the push's host counts to a replacement's held audit entry and releases it for delivery. It reports false when the
// hold had already passed, in which case the entry is delivered without the counts.
func (s *Store) SealAudit(ctx context.Context, id int64, entry auditoutbox.Entry) (bool, error) {
	return s.outbox.Seal(ctx, id, entry)
}
