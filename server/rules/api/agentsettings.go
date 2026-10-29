package api

import "time"

// CommandTypeSetAgentSettings is the command that carries the agent settings to a host (issue #1426). The agent applies them itself.
// Stable wire string; renaming it breaks every deployed agent.
const CommandTypeSetAgentSettings = "set_agent_settings"

// AgentSettings are the fleet-wide switches for what the agent collects (issue #1426).
type AgentSettings struct {
	// Version starts at 0, the settings no operator has changed, and advances by one on every change.
	Version int64 `json:"version"`
	// CapturePrompts is whether agents send the prompts people type to AI agents. On by default.
	CapturePrompts bool `json:"capture_prompts"`
	// UpdatedAt and UpdatedBy are zero until the first change.
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	UpdatedBy string     `json:"updated_by,omitempty"`
	// UpdatedByLabel is the display label the REST handler resolves from UpdatedBy at read time: a user's email, a service
	// account's name, or "system". Empty when it could not be resolved, and never stored.
	UpdatedByLabel string `json:"updated_by_label,omitempty"`
}

// DefaultAgentSettings are the settings before any change: prompts are captured.
func DefaultAgentSettings() AgentSettings {
	return AgentSettings{CapturePrompts: true}
}

// SetAgentSettingsPayload is the set_agent_settings payload. Commands can arrive out of order, so the agent applies one only when its
// (Epoch, Version) is ahead of the last it applied. Epoch is the settings' update time in microseconds, which keeps moving forward
// when a database restore sends Version backwards.
type SetAgentSettingsPayload struct {
	Version        int64 `json:"version"`
	Epoch          int64 `json:"epoch"`
	CapturePrompts bool  `json:"capture_prompts"`
}
