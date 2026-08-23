// Command worker runs the background job processors (River): notifications, outbound
// webhooks, search indexing, automation rules, git ingestion, and observability ingestion.
package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/omni/bugtracker/internal/config"
	"github.com/omni/bugtracker/internal/events"
	"github.com/omni/bugtracker/internal/integrations"
	"github.com/omni/bugtracker/internal/platform"
	"github.com/omni/bugtracker/internal/repo/pg"
	"github.com/omni/bugtracker/internal/worker"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		panic(err)
	}
	logger, logShipper := platform.NewLogger(cfg.Log)
	// Flushes whatever is still queued; a no-op when shipping is off.
	defer logShipper.Close()
	metrics := platform.NewMetrics()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := platform.NewDBPool(ctx, cfg.Database)
	if err != nil {
		logger.Error("db connect", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	store := pg.New(db)
	adapters := integrations.NewRegistry(cfg.Integrations, logger)

	// Register all job workers against the River client and start consuming.
	riverClient, err := worker.New(worker.Deps{
		DB:       db,
		Cfg:      cfg,
		Logger:   logger,
		Metrics:  metrics,
		Store:    store,
		Adapters: adapters,
	})
	if err != nil {
		logger.Error("worker init", "err", err)
		os.Exit(1)
	}

	// Queue depth, queue latency and pool saturation are the numbers worth paging on,
	// and none of them are countable — they are current state, read at scrape time.
	platform.RegisterQueueMetrics(metrics.Registry, db)

	// The worker has to serve its own /metrics.
	//
	// It has always incremented JobsProcessed and WebhookAttempts, and it has never
	// started an HTTP server, so nothing could ever read them: the API exposes its own
	// registry at /metrics, in a different process, where those two series are
	// constant zero. Every dashboard and alert built on job or webhook throughput was
	// therefore reading a flat line and calling it healthy.
	metricsSrv := &http.Server{
		Addr:              cfg.Worker.MetricsAddr,
		Handler:           workerAdminMux(metrics, db),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("worker metrics listening", "addr", metricsSrv.Addr)
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			// Not fatal: losing observability should not take the workers with it.
			logger.Error("worker metrics listen", "err", err)
		}
	}()

	if err := riverClient.Start(ctx); err != nil {
		logger.Error("worker start", "err", err)
		os.Exit(1)
	}
	logger.Info("workers started", "queues", cfg.Worker.Queues)

	<-ctx.Done()
	logger.Info("draining workers")

	// A deadline, because Stop waits for in-flight jobs and background() had none:
	// against a 30s Kubernetes grace period a job that never returns turns a rolling
	// restart into a SIGKILL, which is how a half-finished fan-out gets left behind.
	drainCtx, cancelDrain := context.WithTimeout(context.Background(), workerDrainTimeout)
	defer cancelDrain()
	if err := riverClient.Stop(drainCtx); err != nil {
		logger.Error("worker drain", "err", err)
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	_ = metricsSrv.Shutdown(shutdownCtx)
	_ = events.Noop() // keep events import referenced for wiring parity
}

// workerDrainTimeout bounds how long we wait for in-flight jobs. Comfortably inside a
// default 30s termination grace period, so the process exits on its own terms.
const workerDrainTimeout = 20 * time.Second

// workerAdminMux serves the worker's own metrics and a readiness probe. Deliberately a
// separate listener from anything user-facing — the worker has no public surface, and
// this port belongs to the cluster.
func workerAdminMux(metrics *platform.Metrics, db *pgxpool.Pool) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		w.Header().Set("Content-Type", "application/json")
		if err := db.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			// The reason stays in the log; the body says only that it is not ready.
			// /readyz on the API leaks the raw pgx error, host and user included.
			_, _ = w.Write([]byte(`{"status":"not ready"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
	return mux
}
