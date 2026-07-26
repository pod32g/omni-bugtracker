package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/omni/bugtracker/internal/platform"
)

// RequestLogger emits a structured slog line per request (shipped to Omni-Logging).
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			defer func() {
				logger.LogAttrs(r.Context(), slog.LevelInfo, "http_request",
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Int("status", ww.Status()),
					slog.Int("bytes", ww.BytesWritten()),
					slog.Duration("elapsed", time.Since(start)),
					slog.String("request_id", middleware.GetReqID(r.Context())),
				)
			}()
			next.ServeHTTP(ww, r)
		})
	}
}

// Metrics records request counters and latency into the Prometheus registry.
func Metrics(m *platform.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			route := chiRoutePattern(r)
			m.HTTPDuration.WithLabelValues(route, r.Method).Observe(time.Since(start).Seconds())
			m.HTTPRequests.WithLabelValues(route, r.Method, statusClass(ww.Status())).Inc()
		})
	}
}

// Recoverer converts panics into 500 problem responses without crashing the server.
func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic", "recover", rec, "stack", string(debug.Stack()))
					w.Header().Set("Content-Type", "application/problem+json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"title":"internal error","status":500}`))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// chiRoutePattern returns the matched route template ("/api/v1/issues/{issueKey}"),
// never the concrete path. A Prometheus label must have bounded cardinality: using
// r.URL.Path would mint a new time series per issue key and UUID, so the registry
// would grow without limit for as long as the process runs. Only valid after the
// handler chain has run — chi fills the route context during routing.
func chiRoutePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if p := rctx.RoutePattern(); p != "" {
			return p
		}
	}
	return "unmatched"
}

func statusClass(code int) string {
	switch {
	case code >= 500:
		return "5xx"
	case code >= 400:
		return "4xx"
	case code >= 300:
		return "3xx"
	default:
		return "2xx"
	}
}
