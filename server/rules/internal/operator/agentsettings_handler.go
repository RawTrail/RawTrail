package operator

import (
	"context"
	"errors"
	"net/http"

	identityapi "github.com/rawtrail/rawtrail/server/identity/api"
	"github.com/rawtrail/rawtrail/server/rules/api"
	"github.com/rawtrail/rawtrail/server/rules/internal/agentsettings"
)

// agentSettingsService is the agent-settings surface the detection-config handler consumes; *agentsettings.Service satisfies it.
type agentSettingsService interface {
	Get(ctx context.Context) (api.AgentSettings, error)
	Replace(ctx context.Context, actor *identityapi.Actor, reason string, capturePrompts bool, expectedVersion *int64) (
		agentsettings.ReplaceResult, error)
}

// replaceAgentSettingsRequest is the PUT body. CapturePrompts is a pointer so a request without it is refused rather than read as
// false: a client that misspells the field would otherwise turn prompt capture off for the whole fleet. ExpectedVersion is as for the
// watched-path set.
type replaceAgentSettingsRequest struct {
	CapturePrompts  *bool  `json:"capture_prompts"`
	Reason          string `json:"reason"`
	ExpectedVersion *int64 `json:"expected_version"`
}

// msgCapturePromptsRequired is the refusal for a PUT without capture_prompts.
const msgCapturePromptsRequired = "capture_prompts is required"

// SetAgentSettings wires the agent-settings routes (issue #1426). Set after construction, and only where the command queue and the
// enrolled-host list the push needs are wired.
func (h *DetectionConfigHandler) SetAgentSettings(svc agentSettingsService) {
	h.agentSettings = svc
}

func (h *DetectionConfigHandler) handleGetAgentSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !identityapi.HTTPGate(ctx, w, h.authz, h.logger,
		identityapi.ActionDetectionConfigRead, identityapi.Resource{Type: "detection_config"}) {
		return
	}
	settings, err := h.agentSettings.Get(ctx)
	if err != nil {
		h.logger.ErrorContext(ctx, "detectionconfig get agent settings", "err", err)
		writeDetectionConfigErr(ctx, h.logger, w, http.StatusInternalServerError, errCodeDCInternal, msgDCInternal)
		return
	}
	settings.UpdatedByLabel = h.updatedByLabel(ctx, settings.UpdatedBy)
	writeJSON(ctx, h.logger, w, http.StatusOK, settings)
}

func (h *DetectionConfigHandler) handleReplaceAgentSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !identityapi.HTTPGate(ctx, w, h.authz, h.logger,
		identityapi.ActionDetectionConfigWrite, identityapi.Resource{Type: "detection_config"}) {
		return
	}
	var req replaceAgentSettingsRequest
	if !h.decode(ctx, w, r, &req) {
		return
	}
	if req.CapturePrompts == nil {
		writeDetectionConfigErr(ctx, h.logger, w, http.StatusBadRequest, errCodeDCInvalidInput, msgCapturePromptsRequired)
		return
	}
	actor, ok := h.actor(ctx, w)
	if !ok {
		return
	}
	result, err := h.agentSettings.Replace(ctx, actor, req.Reason, *req.CapturePrompts, req.ExpectedVersion)
	switch {
	case errors.Is(err, agentsettings.ErrVersionConflict):
		writeDetectionConfigErr(ctx, h.logger, w, http.StatusConflict, errCodeDCConflict, err.Error())
	case errors.Is(err, agentsettings.ErrReasonRequired):
		writeDetectionConfigErr(ctx, h.logger, w, http.StatusBadRequest, errCodeDCInvalidInput, msgDCReasonRequired)
	case err != nil:
		h.logger.ErrorContext(ctx, "detectionconfig replace agent settings", "err", err)
		writeDetectionConfigErr(ctx, h.logger, w, http.StatusInternalServerError, errCodeDCInternal, msgDCInternal)
	default:
		result.Settings.UpdatedByLabel = h.updatedByLabel(ctx, result.Settings.UpdatedBy)
		writeJSON(ctx, h.logger, w, http.StatusOK, result)
	}
}
