package service

import (
	"net/http"
	"runtime"
	"time"

	"github.com/omni/bugtracker/internal/auth"
	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/httpapi"
)

// startedAt is process start, for the uptime line. "What is actually deployed" should be
// answerable without SSH.
var startedAt = time.Now()

// limits publishes the server-side bounds a client has to respect before it can act.
// Chiefly the upload cap: the uploader wants to reject an oversize file before spending
// a minute pushing it, and the only alternative to asking is hardcoding a second copy of
// the number in the SPA, which then silently disagrees with the server the first time
// storage.max_upload_mb is tuned. Cheap, no auth beyond the router's, no DB.
func (h *httpHandlers) limits(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"max_upload_bytes": h.maxUpload,
	})
}

// ops serves the operator view. Admin-only: queue contents name the work the instance is
// doing, and delivery URLs are integration detail.
func (h *httpHandlers) ops(w http.ResponseWriter, r *http.Request) {
	if !auth.FromContext(r.Context()).Can(auth.PermAdmin) {
		httpapi.WriteProblem(w, http.StatusForbidden, "forbidden", "missing admin:all")
		return
	}
	snap, err := h.repo.OpsSnapshot(r.Context())
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "ops read failed", err.Error())
		return
	}
	// Empty slices rather than null, so the page can map over them unconditionally.
	if snap.Queues == nil {
		snap.Queues = []domain.QueueState{}
	}
	if snap.Failures == nil {
		snap.Failures = []domain.JobFailure{}
	}
	if snap.Deliveries == nil {
		snap.Deliveries = []domain.DeliveryHealth{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"queues":      snap.Queues,
		"failures":    snap.Failures,
		"deliveries":  snap.Deliveries,
		"queue_error": snap.QueueError,
		"process": map[string]any{
			"uptime_seconds": int(time.Since(startedAt).Seconds()),
			"go_version":     runtime.Version(),
			"goroutines":     runtime.NumGoroutine(),
		},
	})
}
