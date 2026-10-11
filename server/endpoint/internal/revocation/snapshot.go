// Package revocation maintains a per-replica in-memory view of which host enrollments are revoked or have had their token epoch
// bumped, so the agent auth hot path can reject revoked tokens without a per-request database lookup.
//
// Self-validating signed tokens (see internal/signedtoken) prove a token is authentic and unexpired with a local HMAC check and no DB
// access. The one thing a signature cannot prove is "still allowed": an operator may have revoked the host or cycled its credentials
// since the token was minted. This snapshot supplies that answer. It loads the small set of hosts that are revoked or epoch-bumped
// (the overwhelming majority of a fleet is neither, so the map stays tiny even at 100k hosts) and refreshes on a ticker. A host absent
// from the snapshot is allowed; a present host is rejected outright if revoked, or if the presented token's epoch is below the host's
// current epoch.
//
// Per ADR-0010 this is a per-replica perf cache, safe to lose: a fresh replica rebuilds it from the database on startup and on every
// refresh tick, and holds no state a peer replica needs. Revocation is therefore eventually consistent across replicas, bounded by the
// refresh interval. That is the deliberate trade for removing the per-request DB lookup.
//
// The snapshot itself is internal/revocationset, which service-account tokens share. This package adds the host-specific metrics.
package revocation

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/fleetdm/edr/internal/revocationset"
)

const meterName = "github.com/fleetdm/edr/server/endpoint/revocation"

// Entry is one host's revocation state, keyed by host ID. Revoked is terminal (the host is cut off regardless of epoch); Epoch is the
// host's current token_epoch, against which a presented token's epoch is compared.
type Entry = revocationset.Entry

// Source loads the current set of revoked-or-bumped hosts. The mysql store implements it. Only hosts that are revoked or have a
// non-zero token_epoch are returned, so the result set is bounded by the count of cut-off / cycled hosts, not the fleet size.
type Source = revocationset.Source

// Snapshot is a refreshable, concurrent-read view of host revocation state. The service calls Observe on a successful (re-)enrollment
// with the epoch the row now carries: the host's row was reset to not revoked, but its token_epoch is PRESERVED across a re-enroll so
// an operator credential cycle survives, and a token below that epoch stays rejected.
type Snapshot = revocationset.Snapshot

// NewSnapshot constructs a snapshot over src. It starts empty (allows everything) until the first Refresh; callers should Refresh once
// synchronously before serving so a cold replica does not accept an already-revoked token. Registers OTel instruments for size, age,
// and refresh failures so an operator can see the snapshot is live in SigNoz.
func NewSnapshot(src Source, logger *slog.Logger) *Snapshot {
	meter := otel.Meter(meterName)
	refreshFailures, _ := meter.Int64Counter(
		"edr.auth.revocation_snapshot.refresh_failures",
		metric.WithDescription("Revocation snapshot refresh attempts that failed; a sustained non-zero rate means revocations are going stale on this replica."),
		metric.WithUnit("{failure}"),
	)
	s := revocationset.New(src, logger, "revocation snapshot", func(ctx context.Context) { refreshFailures.Add(ctx, 1) })
	_, _ = meter.Int64ObservableGauge(
		"edr.auth.revocation_snapshot.size",
		metric.WithDescription("Number of hosts currently revoked or epoch-bumped in this replica's revocation snapshot."),
		metric.WithUnit("{host}"),
		metric.WithInt64Callback(func(_ context.Context, obs metric.Int64Observer) error {
			obs.Observe(int64(s.Size()))
			return nil
		}),
	)
	_, _ = meter.Float64ObservableGauge(
		"edr.auth.revocation_snapshot.age_seconds",
		metric.WithDescription("Seconds since the revocation snapshot last refreshed successfully on this replica."),
		metric.WithUnit("s"),
		metric.WithFloat64Callback(func(_ context.Context, obs metric.Float64Observer) error {
			if last, ok := s.LastRefresh(); ok {
				obs.Observe(time.Since(last).Seconds())
			}
			return nil
		}),
	)
	return s
}
