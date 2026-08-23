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
