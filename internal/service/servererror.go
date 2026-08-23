package service

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/omni/bugtracker/internal/httpapi"
)

// serverError is the single way this package answers a 5xx.
//
// It replaces ~110 copies of a call that passed err.Error() straight through as the
// problem detail, which got the split exactly backwards: the error text went to the client and nowhere
// else. That put Postgres constraint names, fragments of SQL and absolute filesystem
// paths in front of whoever made the request, while the operator — the only person who
// can act on any of it — had nothing but a status code in the access log.
//
// So: the error goes to the log, with the request id and the route that produced it.
// The client gets the same request id and nothing else, which is the one detail that
// makes a support conversation short ("it said c8f3a1b2") without describing the
// schema to a stranger.
func writeServerError(w http.ResponseWriter, r *http.Request, log *slog.Logger, title string, err error) {
	reqID := middleware.GetReqID(r.Context())
	if log != nil {
		log.Error(title,
			slog.String("err", err.Error()),
			slog.String("request_id", reqID),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
		)
	}
	detail := "the server failed to handle this request"
	if reqID != "" {
		// Quoted so it survives being copied out of a UI toast.
		detail += ` (request id "` + reqID + `")`
	}
	httpapi.WriteProblem(w, http.StatusInternalServerError, title, detail)
}

func (h *httpHandlers) serverError(w http.ResponseWriter, r *http.Request, title string, err error) {
	writeServerError(w, r, h.log, title, err)
}

// The inbound-integration handlers are a separate type with their own logger, but the
// endpoints are just as capable of returning a database error to a caller — and their
// callers are other systems, which will never read a detail string but will happily
// log one somewhere else.
func (h *IntegrationHandlers) serverError(w http.ResponseWriter, r *http.Request, title string, err error) {
	writeServerError(w, r, h.logger, title, err)
}
