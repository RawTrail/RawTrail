package agentsettings

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/rawtrail/rawtrail/server/rules/api"
	"github.com/rawtrail/rawtrail/server/rules/internal/hostpush"
)

// Converger queues the current settings for hosts that do not have them. Settings no operator has changed are sent too: an agent with
// no settings of its own captures no prompts until it has the fleet's, so that a fleet that turned capture off never has it turned on
// by an agent that does not know yet.
type Converger = hostpush.Converger[api.AgentSettings]

// NewConverger builds a Converger. Every dependency but logger is required.
func NewConverger(store *Store, commands func(ctx context.Context, hostIDs []string, commandType string, payload []byte) (int, error),
	enrollments api.HostEnrollmentLister, latest api.HostCommandLister, logger *slog.Logger) *Converger {
	if store == nil {
		panic("agentsettings.NewConverger: store is required")
	}
	return hostpush.NewConverger(hostpush.ConvergerConfig[api.AgentSettings]{
		Subject: subject, CommandType: api.CommandTypeSetAgentSettings, Get: store.Get,
		Version: func(s api.AgentSettings) int64 { return s.Version }, Payload: commandPayload, Carries: carries,
		Commands: commands, Enrollments: enrollments, Latest: latest, Logger: logger,
	})
}

// carries reports whether a command's payload delivers the current settings. The same version with a different epoch is different
// settings, as after a database restore that sent versions backwards.
func carries(payload []byte, settings api.AgentSettings) bool {
	var queued api.SetAgentSettingsPayload
	return json.Unmarshal(payload, &queued) == nil &&
		queued.Version == settings.Version && queued.Epoch == epoch(settings)
}
