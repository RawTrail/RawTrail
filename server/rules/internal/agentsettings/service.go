package agentsettings

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

// ErrReasonRequired is returned for a change without a reason. Every change is audited with its reason, and turning prompt capture
// on or off for a fleet is a decision a reviewer will want explained.
var ErrReasonRequired = errors.New("agentsettings: reason is required")

const subject = "agentsettings"

// Service stores the agent settings and pushes them to hosts.
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
		panic("agentsettings.NewService: store, commands and hosts are required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	push := hostpush.Pusher{Subject: subject, CommandType: api.CommandTypeSetAgentSettings, Commands: commands, Hosts: hosts, Logger: logger}
	return &Service{store: store, push: push, drain: drain}
}

// Get returns the stored settings.
func (s *Service) Get(ctx context.Context) (api.AgentSettings, error) {
	return s.store.Get(ctx)
}

// ReplaceResult is replaced settings and how their push to hosts went.
type ReplaceResult struct {
	Settings api.AgentSettings `json:"settings"`
	hostpush.Counts
}

// Replace stores the settings, queues a set_agent_settings command for every enrolled host, and audits the change with its reason.
// As for the watched-path set, the stored settings are authoritative once written: a push that misses hosts does not fail the change,
// the result and the audit row count the hosts it missed, and the catch-up sends it to them.
//
// A non-nil expectedVersion is the version the caller's edit started from; the change is refused with ErrVersionConflict when the
// settings have moved on.
func (s *Service) Replace(
	ctx context.Context, actor *identityapi.Actor, reason string, capturePrompts bool, expectedVersion *int64,
) (ReplaceResult, error) {
	if strings.TrimSpace(reason) == "" {
		return ReplaceResult{}, ErrReasonRequired
	}
	previous, settings, auditID, err := s.store.Replace(ctx, capturePrompts, actor.Principal.ID, expectedVersion,
		func(previous, next api.AgentSettings) (auditoutbox.Entry, error) {
			return auditEntry(ctx, actor, reason, previous, ReplaceResult{Settings: next}, false)
		})
	if err != nil {
		return ReplaceResult{}, err
	}
	result := ReplaceResult{Settings: settings, Counts: s.push.Fanout(ctx, settings.Version, commandPayload(settings))}
	entry, err := auditEntry(ctx, actor, reason, previous, result, true)
	s.push.CompleteAudit(ctx, settings.Version, auditID, entry, err, s.store.SealAudit, s.drain)
	return result, nil
}

// commandPayload is the set_agent_settings payload for stored settings. The push and the catch-up both build it here. The settings
// must have been changed at least once, so they carry their update time. Marshalling a struct of integers and a boolean cannot fail.
func commandPayload(settings api.AgentSettings) []byte {
	payload, _ := json.Marshal(api.SetAgentSettingsPayload{
		Version: settings.Version, Epoch: settings.UpdatedAt.UnixMicro(), CapturePrompts: settings.CapturePrompts,
	})
	return payload
}

// auditEntry encodes a replacement's audit row: the reason, the previous and new settings, and, when withCounts, how the push went.
func auditEntry(
	ctx context.Context, actor *identityapi.Actor, reason string, previous api.AgentSettings, result ReplaceResult, withCounts bool,
) (auditoutbox.Entry, error) {
	settings := result.Settings
	payload := map[string]any{
		"reason":                   reason,
		"version":                  settings.Version,
		"capture_prompts":          settings.CapturePrompts,
		"previous_version":         previous.Version,
		"previous_capture_prompts": previous.CapturePrompts,
	}
	if withCounts {
		result.AuditPayload(payload)
	}
	return auditoutbox.Encode(identityapi.AuditEvent{
		Actor:      actor.Principal,
		Action:     identityapi.AuditDetectionConfigAgentSettingsUpdate,
		TargetType: "agent_settings",
		TargetID:   strconv.FormatInt(settings.Version, 10),
		TraceID:    identityapi.TraceIDFromContext(ctx),
		Payload:    payload,
	})
}
