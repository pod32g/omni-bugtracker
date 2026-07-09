package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/omni/bugtracker/internal/auth"
	"github.com/omni/bugtracker/internal/config"
	mw "github.com/omni/bugtracker/internal/httpapi/middleware"
	"github.com/omni/bugtracker/internal/platform"
)

// Deps are everything the HTTP layer needs, injected from cmd/server.
type Deps struct {
	Cfg      *config.Config
	Logger   *slog.Logger
	Metrics  *platform.Metrics
	DB       *pgxpool.Pool
	Verifier *auth.Verifier
	Authn    mw.Authenticator
	// Handlers is the OpenAPI strict server implementation, mounted under /api/v1
	// once `make generate` has produced the httpgen package. See mountGenerated().
	Handlers http.Handler
	// AuthFlow serves the unauthenticated browser login endpoints (/auth/*).
	AuthFlow http.Handler
	// InboundGit handles HMAC-verified git provider webhooks.
	InboundGit http.HandlerFunc
}

// NewRouter builds the fully-wired chi router.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(mw.Recoverer(d.Logger))
	r.Use(mw.RequestLogger(d.Logger))
	r.Use(mw.Metrics(d.Metrics))
	r.Use(chimw.Timeout(60 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   d.Cfg.Server.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "Idempotency-Key"},
		AllowCredentials: true,
	}))

	// Unauthenticated operational endpoints.
	r.Get("/healthz", health)
	r.Get("/readyz", readyz(d.DB))
	r.Handle("/metrics", promhttp.HandlerFor(d.Metrics.Registry, promhttp.HandlerOpts{}))

	// Unauthenticated browser login flow (OIDC BFF).
	if d.AuthFlow != nil {
		r.Mount("/auth", d.AuthFlow)
	}

	// Authenticated API surface.
	r.Route("/api/v1", func(api chi.Router) {
		// Inbound integration webhooks authenticate via HMAC, not bearer — mounted first.
		mountInboundIntegrations(api, d)

		api.Group(func(secured chi.Router) {
			secured.Use(mw.Auth(d.Verifier, d.Authn))
			mountGenerated(secured, d)
		})
	})

	return r
}

func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func readyz(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(r.Context()); err != nil {
			WriteProblem(w, http.StatusServiceUnavailable, "not ready", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	}
}

// mountGenerated attaches the OpenAPI strict-server handlers. Until `make generate`
// runs, d.Handlers is nil and we expose a clear placeholder so the server still boots.
func mountGenerated(r chi.Router, d Deps) {
	if d.Handlers != nil {
		r.Mount("/", d.Handlers)
		return
	}
	r.HandleFunc("/*", func(w http.ResponseWriter, _ *http.Request) {
		WriteProblem(w, http.StatusNotImplemented, "not generated",
			"run `make generate` to produce the OpenAPI handlers")
	})
}

// mountInboundIntegrations wires HMAC-verified inbound endpoints (git / logging / metrics).
// Concrete handlers land in internal/httpapi/handlers once the service layer is generated.
func mountInboundIntegrations(r chi.Router, d Deps) {
	git := notImplemented
	if d.InboundGit != nil {
		git = d.InboundGit
	}
	r.Post("/integrations/git/events", git)
	r.Post("/integrations/logging/alerts", notImplemented)
	r.Post("/integrations/metrics/alerts", notImplemented)
}

func notImplemented(w http.ResponseWriter, _ *http.Request) {
	WriteProblem(w, http.StatusNotImplemented, "not implemented",
		"handler pending service-layer wiring")
}
