// Package credentialguard stores the credential guard's policy and pushes it to hosts (issue #1432): the guard's mode and the
// credential files it protects. It reaches hosts the way the agent settings do (hostpush); what is here is its storage, payload and
// audit row.
package credentialguard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/jmoiron/sqlx"

	"github.com/rawtrail/rawtrail/internal/versionedset"
	"github.com/rawtrail/rawtrail/server/auditoutbox"
	"github.com/rawtrail/rawtrail/server/rules/api"
	"github.com/rawtrail/rawtrail/server/rules/internal/detectionconfig"
	"github.com/rawtrail/rawtrail/server/rules/internal/hostpush"
)

// Store reads and replaces the single credential_guard row. Its concurrency is versionedset's.
type Store struct {
	*hostpush.Document[api.CredentialGuardPolicy]
}

// NewStore builds a Store. Panics on a nil db, which is a wiring bug.
func NewStore(db *sqlx.DB) *Store {
	table := versionedset.Table{Name: "credential_guard", PayloadColumn: "policy", Noun: "credential guard policy"}
	return &Store{hostpush.NewDocument(db, table, detectionconfig.AuditOutboxTable, decode, ErrVersionConflict)}
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

// Replace stores mode and credentials as the new policy and returns the policy it replaced and the new one, one version past it.
// A non-nil expectedVersion makes the replacement conditional, as for the agent settings, and the audit entry audit builds is written
// in the same transaction, held for hostpush.AuditHold so the caller can add the push's host counts with SealAudit.
func (s *Store) Replace(
	ctx context.Context, mode string, credentials []api.ProtectedCredential, actor string, expectedVersion *int64,
	audit func(previous, next api.CredentialGuardPolicy) (auditoutbox.Entry, error),
) (previous, next api.CredentialGuardPolicy, auditID int64, err error) {
	// An absent list, or a credential's absent owners, is stored as an empty one, so a reader and the extension see an array.
	credentials = slices.Clone(credentials)
	if credentials == nil {
		credentials = []api.ProtectedCredential{}
	}
	for i := range credentials {
		if credentials[i].Owners == nil {
			credentials[i].Owners = []api.CredentialOwner{}
		}
	}
	encoded, err := json.Marshal(stored{Mode: mode, Credentials: credentials})
	if err != nil {
		return api.CredentialGuardPolicy{}, api.CredentialGuardPolicy{}, 0, fmt.Errorf("encode credential guard policy: %w", err)
	}
	return s.ReplaceEncoded(ctx, encoded, actor, expectedVersion, audit)
}
