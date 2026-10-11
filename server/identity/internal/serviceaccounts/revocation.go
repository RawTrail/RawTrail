package serviceaccounts

import (
	"log/slog"
	"time"

	"github.com/fleetdm/edr/internal/revocationset"
)

// DefaultRevocationRefreshInterval is how often each replica reloads the revocation snapshot from the database. It bounds the
// revocation staleness window (how long a just-revoked service account's outstanding access token keeps validating on a replica)
// against hammering the DB. Mirrors the host-token snapshot cadence (#454).
const DefaultRevocationRefreshInterval = 5 * time.Second

// Entry is one service account's revocation state, keyed by client ID. Revoked is terminal; Epoch is the account's current epoch,
// against which a presented token's epoch is compared.
type Entry = revocationset.Entry

// Source loads the current set of revoked-or-bumped service accounts. The Store implements it.
type Source = revocationset.Source

// Snapshot is a refreshable, concurrent-read view of service-account revocation state, shared with host tokens through
// internal/revocationset. Per ADR-0010 it is a per-replica performance cache that is safe to lose: it rebuilds from MySQL on every
// refresh, and holds only revoked or rotated accounts, so a lost snapshot fails open until the next refresh.
type Snapshot = revocationset.Snapshot

// NewSnapshot constructs a snapshot over src. It starts empty (allows everything) until the first Refresh; callers should Refresh once
// synchronously before serving so a cold replica does not accept an already-revoked token.
func NewSnapshot(src Source, logger *slog.Logger) *Snapshot {
	return revocationset.New(src, logger, "service-account revocation snapshot", nil)
}
