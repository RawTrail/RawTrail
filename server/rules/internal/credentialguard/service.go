package credentialguard

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"

	"github.com/rawtrail/rawtrail/server/auditoutbox"
	identityapi "github.com/rawtrail/rawtrail/server/identity/api"
	"github.com/rawtrail/rawtrail/server/rules/api"
	"github.com/rawtrail/rawtrail/server/rules/internal/appcontrol"
	"github.com/rawtrail/rawtrail/server/rules/internal/hostpush"
)

// ErrReasonRequired is returned for a change without a reason. Switching the guard to protect, or off, changes what AI agents on every
// host may read, and a reviewer will want it explained.
var ErrReasonRequired = errors.New("credentialguard: reason is required")

// InvalidPolicyError is a policy that fails api.ValidateCredentialGuard. Its message names the field.
type InvalidPolicyError struct{ Err error }

func (e InvalidPolicyError) Error() string { return e.Err.Error() }
func (e InvalidPolicyError) Unwrap() error { return e.Err }

const subject = "credentialguard"

// Service stores the credential guard's policy and pushes it to hosts.
type Service struct {
	store *Store
	push  hostpush.Pusher
	drain *auditoutbox.Drain
}

// NewService builds a Service. store, commands and hosts are required. drain delivers the audit entries changes commit; it may be nil
// outside production, which leaves them in the outbox.
func NewService(store *Store, commands appcontrol.CommandBatchInserter, hosts appcontrol.HostLister, drain *auditoutbox.Drain,
	logger *slog.Logger) *Service {
	if store == nil || commands == nil || hosts == nil {
		panic("credentialguard.NewService: store, commands and hosts are required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	push := hostpush.Pusher{Subject: subject, CommandType: api.CommandTypeSetCredentialGuard, Commands: commands, Hosts: hosts,
		Logger: logger}
	return &Service{store: store, push: push, drain: drain}
}

// Get returns the stored policy.
func (s *Service) Get(ctx context.Context) (api.CredentialGuardPolicy, error) {
	return s.store.Get(ctx)
}

// ReplaceResult is a replaced policy and how its push to hosts went.
type ReplaceResult struct {
	Policy api.CredentialGuardPolicy `json:"policy"`
	hostpush.Counts
}

// Replace validates and stores the policy, queues a set_credential_guard command for every enrolled host, and audits the change with
// its reason. As for the agent settings, the stored policy is authoritative once written: a push that misses hosts does not fail the
// change, and the catch-up sends it to them. A non-nil expectedVersion refuses the change with ErrVersionConflict when the policy has
// moved on since the caller read it.
func (s *Service) Replace(
	ctx context.Context, actor *identityapi.Actor, reason, mode string, credentials []api.ProtectedCredential, expectedVersion *int64,
) (ReplaceResult, error) {
	if strings.TrimSpace(reason) == "" {
		return ReplaceResult{}, ErrReasonRequired
	}
	if err := api.ValidateCredentialGuard(mode, credentials); err != nil {
		return ReplaceResult{}, InvalidPolicyError{Err: err}
	}
	previous, policy, auditID, err := s.store.Replace(ctx, mode, credentials, actor.Principal.ID, expectedVersion,
		func(previous, next api.CredentialGuardPolicy) (auditoutbox.Entry, error) {
			return auditEntry(ctx, actor, reason, previous, ReplaceResult{Policy: next}, false)
		})
	if err != nil {
		return ReplaceResult{}, err
	}
	result := ReplaceResult{Policy: policy, Counts: s.push.Fanout(ctx, policy.Version, commandPayload(policy))}
	entry, err := auditEntry(ctx, actor, reason, previous, result, true)
	s.push.CompleteAudit(ctx, policy.Version, auditID, entry, err, s.store.SealAudit, s.drain)
	return result, nil
}

// commandPayload is the set_credential_guard payload for a stored policy. The push and the catch-up both build it here. Marshalling a
// struct of strings, integers and booleans cannot fail.
func commandPayload(policy api.CredentialGuardPolicy) []byte {
	payload, _ := json.Marshal(api.SetCredentialGuardPayload{
		Version: policy.Version, Epoch: epoch(policy), Mode: policy.Mode, Credentials: policy.Credentials,
	})
	return payload
}

// epoch is the policy's update time in Unix microseconds, or 0 for the policy no operator has changed, which has none.
func epoch(policy api.CredentialGuardPolicy) int64 {
	if policy.UpdatedAt == nil {
		return 0
	}
	return policy.UpdatedAt.UnixMicro()
}

// auditEntry encodes a replacement's audit row: the reason, the previous and new mode and credentials, and, when withCounts, how the
// push went.
func auditEntry(
	ctx context.Context, actor *identityapi.Actor, reason string, previous api.CredentialGuardPolicy, result ReplaceResult,
	withCounts bool,
) (auditoutbox.Entry, error) {
	policy := result.Policy
	payload := map[string]any{
		"reason":               reason,
		"version":              policy.Version,
		"mode":                 policy.Mode,
		"credentials":          policy.Credentials,
		"previous_version":     previous.Version,
		"previous_mode":        previous.Mode,
		"previous_credentials": previous.Credentials,
	}
	if withCounts {
		result.AuditPayload(payload)
	}
	return auditoutbox.Encode(identityapi.AuditEvent{
		Actor:      actor.Principal,
		Action:     identityapi.AuditDetectionConfigCredentialGuardUpdate,
		TargetType: "credential_guard",
		TargetID:   strconv.FormatInt(policy.Version, 10),
		TraceID:    identityapi.TraceIDFromContext(ctx),
		Payload:    payload,
	})
}
