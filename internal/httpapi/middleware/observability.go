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

// slowRequest is the latency above which a request stops being routine and becomes
// something worth reading about. Deliberately well clear of normal API work so this
// fires on genuine stalls, not on a merely busy query. A var only so tests can lower
// it and exercise the branch without sleeping for two seconds.
var slowRequest = 2 * time.Second

// opsPaths are scraped and probed on a timer by machines, not called by users. They
// are the single largest source of log volume (Prometheus alone hits /metrics every
// 15s, once accounting for half of everything we shipped) and they say nothing: the
// scrape either succeeds, or Prometheus itself alerts that the target is down.
var opsPaths = map[string]bool{"/metrics": true, "/healthz": true, "/readyz": true}

// RequestLogger records requests that are *events*. A request that succeeded is not an
// event, it is a measurement, and we already take that measurement properly — Metrics
// below counts every request by route, method and status class, which answers "how many
// / how fast / how many failed" far better than a line of text per hit ever could.
//
// So the levels here describe the outcome rather than the traffic: a 5xx is a failure we
// want to read, an unusually slow request is a symptom, and everything else is available
// at debug when you are actually debugging. This is the difference between a log that
// tells you what went wrong and one that just proves the server is up.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if opsPaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			defer func() {
				elapsed := time.Since(start)
				level, msg := slog.LevelDebug, "http_request"
				switch {
				case ww.Status() >= 500:
					level, msg = slog.LevelError, "http_request_failed"
				case elapsed >= slowRequest:
					level, msg = slog.LevelWarn, "http_request_slow"
				}
				logger.LogAttrs(r.Context(), level, msg,
					slog.String("method", r.Method),
					slog.String("route", chiRoutePattern(r)),
					slog.String("path", r.URL.Path),
					slog.Int("status", ww.Status()),
					slog.Int("bytes", ww.BytesWritten()),
					// Milliseconds, not slog.Duration: that marshals to raw nanoseconds
					// in JSON ("elapsed":1458746), which no log viewer reads as a time.
					slog.Int64("elapsed_ms", elapsed.Milliseconds()),
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
