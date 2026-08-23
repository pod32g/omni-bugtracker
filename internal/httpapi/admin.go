package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// AdminMux is the operator-facing surface: Prometheus scrapes and the container
// probes. It is deliberately a *different listener* from the API.
//
// /metrics used to hang off the public router next to /healthz, outside the
// authenticated group, which on a published deployment answered an unauthenticated
// GET with ~74KB describing the install: every route pattern the API exposes, request
// counts and latencies per route and status, issue-creation counts by type and source,
// rate-limit outcomes, and Go runtime internals. None of it is issue content, but
// together it is a map of the API surface and how hard it is being used, handed to
// anyone who can reach the port.
//
// Prometheus scrapes from inside the cluster, so the fix is topological rather than
// cryptographic: bind this to an address that is not published. There is no bearer
// token here because a scrape config that has to carry one tends to end up carrying an
// admin token, and the port is not reachable in the first place.
func AdminMux(reg *prometheus.Registry, db *pgxpool.Pool, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	if reg != nil {
		mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		// A scrape deadline of its own: probing readiness must not be able to block
		// on a database that is refusing connections slowly.
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if db == nil {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ready"}`))
			return
		}
		if err := db.Ping(ctx); err != nil {
			if logger != nil {
				logger.Error("readiness check failed", "err", err)
			}
			// The reason stays in the log. A pgx connection error names the host,
			// the port, the user and the database.
			WriteProblem(w, http.StatusServiceUnavailable, "not ready", "the database is unreachable")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
	return mux
}

// AdminServer wraps AdminMux in a server with a header timeout, ready to be started.
func AdminServer(addr string, reg *prometheus.Registry, db *pgxpool.Pool, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           AdminMux(reg, db, logger),
		ReadHeaderTimeout: 5 * time.Second,
	}
}
