package operator

import (
	"context"
	"errors"
	"net/http"

	identityapi "github.com/rawtrail/rawtrail/server/identity/api"
	"github.com/rawtrail/rawtrail/server/rules/api"
	"github.com/rawtrail/rawtrail/server/rules/internal/credentialguard"
)

// credentialGuardService is the credential-guard surface the detection-config handler consumes; *credentialguard.Service satisfies
// it.
type credentialGuardService interface {
	Get(ctx context.Context) (api.CredentialGuardPolicy, error)
	Replace(ctx context.Context, actor *identityapi.Actor, reason, mode string, credentials []api.ProtectedCredential,
		expectedVersion *int64) (credentialguard.ReplaceResult, error)
}

// replaceCredentialGuardRequest is the PUT body. Credentials is a pointer so a request without it is refused rather than read as an
// empty list: a client that misspells the field would otherwise stop the guard protecting anything. ExpectedVersion is as for the
// agent settings.
type replaceCredentialGuardRequest struct {
	Mode            string                     `json:"mode"`
	Credentials     *[]api.ProtectedCredential `json:"credentials"`
	Reason          string                     `json:"reason"`
	ExpectedVersion *int64                     `json:"expected_version"`
}

// msgCredentialsRequired is the refusal for a PUT without credentials.
const msgCredentialsRequired = "credentials is required; send [] to protect nothing"

// SetCredentialGuard wires the credential-guard routes (issue #1432). Set after construction, and only where the command queue and
// the enrolled-host list the push needs are wired.
func (h *DetectionConfigHandler) SetCredentialGuard(svc credentialGuardService) {
	h.credentialGuard = svc
}

func (h *DetectionConfigHandler) handleGetCredentialGuard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !identityapi.HTTPGate(ctx, w, h.authz, h.logger,
		identityapi.ActionDetectionConfigRead, identityapi.Resource{Type: "detection_config"}) {
		return
	}
	policy, err := h.credentialGuard.Get(ctx)
	if err != nil {
		h.logger.ErrorContext(ctx, "detectionconfig get credential guard", "err", err)
		writeDetectionConfigErr(ctx, h.logger, w, http.StatusInternalServerError, errCodeDCInternal, msgDCInternal)
		return
	}
	policy.UpdatedByLabel = h.updatedByLabel(ctx, policy.UpdatedBy)
	writeJSON(ctx, h.logger, w, http.StatusOK, policy)
}

func (h *DetectionConfigHandler) handleReplaceCredentialGuard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !identityapi.HTTPGate(ctx, w, h.authz, h.logger,
		identityapi.ActionDetectionConfigWrite, identityapi.Resource{Type: "detection_config"}) {
		return
	}
	var req replaceCredentialGuardRequest
	if !h.decode(ctx, w, r, &req) {
		return
	}
	if req.Credentials == nil {
		writeDetectionConfigErr(ctx, h.logger, w, http.StatusBadRequest, errCodeDCInvalidInput, msgCredentialsRequired)
		return
	}
	actor, ok := h.actor(ctx, w)
	if !ok {
		return
	}
	result, err := h.credentialGuard.Replace(ctx, actor, req.Reason, req.Mode, *req.Credentials, req.ExpectedVersion)
	var invalid credentialguard.InvalidPolicyError
	switch {
	case errors.Is(err, credentialguard.ErrVersionConflict):
		writeDetectionConfigErr(ctx, h.logger, w, http.StatusConflict, errCodeDCConflict, err.Error())
	case errors.Is(err, credentialguard.ErrReasonRequired):
		writeDetectionConfigErr(ctx, h.logger, w, http.StatusBadRequest, errCodeDCInvalidInput, msgDCReasonRequired)
	case errors.As(err, &invalid):
		writeDetectionConfigErr(ctx, h.logger, w, http.StatusBadRequest, errCodeDCInvalidInput, invalid.Error())
	case err != nil:
		h.logger.ErrorContext(ctx, "detectionconfig replace credential guard", "err", err)
		writeDetectionConfigErr(ctx, h.logger, w, http.StatusInternalServerError, errCodeDCInternal, msgDCInternal)
	default:
		result.Policy.UpdatedByLabel = h.updatedByLabel(ctx, result.Policy.UpdatedBy)
		writeJSON(ctx, h.logger, w, http.StatusOK, result)
	}
}
