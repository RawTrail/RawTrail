package credentialguard

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/rawtrail/rawtrail/server/rules/api"
	"github.com/rawtrail/rawtrail/server/rules/internal/hostpush"
)

// Converger queues the current policy for hosts that do not have it. The policy no operator has changed is sent too, so a host learns
// the fleet's mode and list rather than keeping whatever its extension last had.
type Converger = hostpush.Converger[api.CredentialGuardPolicy]

// NewConverger builds a Converger. Every dependency but logger is required.
func NewConverger(store *Store, commands func(ctx context.Context, hostIDs []string, commandType string, payload []byte) (int, error),
	enrollments api.HostEnrollmentLister, latest api.HostCommandLister, logger *slog.Logger) *Converger {
	if store == nil {
		panic("credentialguard.NewConverger: store is required")
	}
	return hostpush.NewConverger(hostpush.ConvergerConfig[api.CredentialGuardPolicy]{
		Subject: subject, CommandType: api.CommandTypeSetCredentialGuard, Get: store.Get,
		Version: func(p api.CredentialGuardPolicy) int64 { return p.Version }, Payload: commandPayload, Carries: carries,
		Commands: commands, Enrollments: enrollments, Latest: latest, Logger: logger,
	})
}

// carries reports whether a command's payload delivers the current policy. The same version with a different epoch is a different
// policy, as after a database restore that sent versions backwards.
func carries(payload []byte, policy api.CredentialGuardPolicy) bool {
	var queued api.SetCredentialGuardPayload
	return json.Unmarshal(payload, &queued) == nil &&
		queued.Version == policy.Version && queued.Epoch == epoch(policy)
}
