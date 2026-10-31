package operator

import (
	"context"
	"net/http"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/httpserver"
	identityapi "github.com/rawtrail/rawtrail/server/identity/api"
)

// StoreSizeReader reads how much disk the event archive and the database take (issue #1434). mysql.Store satisfies it.
type StoreSizeReader interface {
	StoreSizes(ctx context.Context) (api.StoreSizes, error)
}

// bytesPerGB converts the warning threshold, set in GB, to bytes. Decimal, as disk sizes are sold and as the console shows them.
const bytesPerGB = 1_000_000_000

// storageSettings is what the storage report states beside the sizes: how long events are kept, and the size it warns past.
type storageSettings struct {
	reader             StoreSizeReader
	eventRetentionDays int
	warnBytes          int64
}

// SetStorage installs the storage report: the sizes' read, the event retention (EDR_EVENT_RETENTION_DAYS) and the size past which it
// warns, in GB (EDR_STORE_SIZE_WARN_GB, 0 never warns). Unset, the route answers 503.
func (h *Handler) SetStorage(reader StoreSizeReader, eventRetentionDays, warnGB int) {
	h.storage = &storageSettings{reader: reader, eventRetentionDays: eventRetentionDays, warnBytes: int64(warnGB) * bytesPerGB}
}

// storageResponse is GET /api/v1/admin/storage.
type storageResponse struct {
	EventArchiveBytes  int64 `json:"event_archive_bytes"`
	DatabaseBytes      int64 `json:"database_bytes"`
	TotalBytes         int64 `json:"total_bytes"`
	EventRetentionDays int   `json:"event_retention_days"`
	// WarnBytes is the size past which the store is reported as large, 0 when it never is.
	WarnBytes int64 `json:"warn_bytes"`
	OverWarn  bool  `json:"over_warn"`
}

func (h *Handler) registerStorageRoutes(mux httpserver.Router) {
	mux.HandleFunc("GET /api/v1/admin/storage", h.handleStorage)
}

// handleStorage reports how much disk the server's stores take, for the admin settings page where retention is set. It reads the
// same as the detection settings, since it is shown beside them.
func (h *Handler) handleStorage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !identityapi.HTTPGate(ctx, w, h.authz, h.logger,
		identityapi.ActionDetectionConfigRead, identityapi.Resource{Type: "detection_config"}) {
		return
	}
	if h.storage == nil {
		h.writeError(ctx, w, http.StatusServiceUnavailable, errInternal)
		return
	}
	sizes, err := h.storage.reader.StoreSizes(ctx)
	if err != nil {
		h.logger.ErrorContext(ctx, "detection storage sizes", "err", err)
		h.writeError(ctx, w, http.StatusInternalServerError, errInternal)
		return
	}
	total := sizes.EventArchiveBytes + sizes.DatabaseBytes
	h.writeJSON(w, r, storageResponse{
		EventArchiveBytes: sizes.EventArchiveBytes, DatabaseBytes: sizes.DatabaseBytes, TotalBytes: total,
		EventRetentionDays: h.storage.eventRetentionDays, WarnBytes: h.storage.warnBytes,
		OverWarn: h.storage.warnBytes > 0 && total > h.storage.warnBytes,
	})
}
