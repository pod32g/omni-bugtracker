package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/omni/bugtracker/internal/config"
	"github.com/omni/bugtracker/internal/platform"
)

// The scrape payload is a map of the install — every route pattern, per-route traffic,
// runtime internals. It must not be reachable from the listener that is published.
func TestPublicRouterDoesNotServeMetrics(t *testing.T) {
	rec := httptest.NewRecorder()
	publicRouter(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "bugtracker_http_requests_total") {
		t.Fatal("/metrics served the Prometheus registry on the public listener")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/metrics = %d, want 404", rec.Code)
	}
}

// ...but the probes stay, because the container runtime cannot authenticate.
func TestPublicRouterStillServesProbes(t *testing.T) {
	rec := httptest.NewRecorder()
	publicRouter(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz = %d, want 200", rec.Code)
	}
}

func TestAdminMuxServesMetrics(t *testing.T) {
	m := platform.NewMetrics()
	m.HTTPRequests.WithLabelValues("/x", "GET", "200").Inc()

	rec := httptest.NewRecorder()
	AdminMux(m.Registry, nil, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin /metrics = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "bugtracker_http_requests_total") {
		t.Fatal("admin /metrics did not expose the application registry")
	}
}

func publicRouter(t *testing.T) http.Handler {
	t.Helper()
	return NewRouter(Deps{
		Cfg:     &config.Config{},
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Metrics: platform.NewMetrics(),
	})
}

// The spec declares /healthz and /readyz with `security: []` under `servers: /api/v1`,
// so the published URL is /api/v1/healthz. The handlers were only ever on the root
// router, and /api/v1/healthz fell into the authenticated group — an uptime monitor, a
// load balancer or a Kubernetes probe configured from the docs got 401 and reported
// the service as down.
func TestProbesAreServedAtTheDocumentedPath(t *testing.T) {
	r := publicRouter(t)
	for _, path := range []string{"/healthz", "/api/v1/healthz"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200 (unauthenticated)", path, rec.Code)
		}
	}
	// /readyz pings the database, which is nil here, so the status depends on the
	// pool. What matters is that it is not the 401 the auth group would produce.
	for _, path := range []string{"/readyz", "/api/v1/readyz"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusNotFound {
			t.Errorf("GET %s = %d — the documented probe is behind auth or missing", path, rec.Code)
		}
	}
}
