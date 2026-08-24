// Command server runs the Omni-BugTracker HTTP API.
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

	"github.com/omni/bugtracker/internal/auth"
	"github.com/omni/bugtracker/internal/config"
	"github.com/omni/bugtracker/internal/events"
	"github.com/omni/bugtracker/internal/httpapi"
	mw "github.com/omni/bugtracker/internal/httpapi/middleware"
	"github.com/omni/bugtracker/internal/platform"
	"github.com/omni/bugtracker/internal/repo/pg"
	"github.com/omni/bugtracker/internal/service"
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

	// Redis backs rate limiting only — never a source of truth. If it is unreachable
	// the limiter stays nil and the API serves unlimited rather than not at all.
	var limiter mw.Limiter
	if rdb, err := platform.NewRedis(ctx, cfg.Redis); err != nil {
		logger.Warn("redis unavailable — rate limiting disabled", "err", err)
	} else {
		defer func() { _ = rdb.Close() }()
		limiter = mw.NewRedisLimiter(rdb)
	}

	// River insert-only client: enqueue event-dispatch jobs transactionally on writes.
	publisher, err := events.NewPublisher(db)
	if err != nil {
		logger.Error("river publisher", "err", err)
		os.Exit(1)
	}

	store := pg.New(db)
	verifier := auth.NewVerifier(cfg.Identity)
	authn := service.NewAuth(store)

	// The generated OpenAPI strict handlers are wired here once `make generate` runs.
	handlers := service.NewHTTPHandlers(store, publisher, logger, cfg)
	authFlow := service.NewOIDC(cfg.Identity, store, logger).Router()
	integrations := service.NewIntegrationHandlers(publisher, store, cfg.Integrations, logger)

	router := httpapi.NewRouter(httpapi.Deps{
		Cfg:            cfg,
		Logger:         logger,
		Metrics:        metrics,
		DB:             db,
		Verifier:       verifier,
		Authn:          authn,
		Limiter:        limiter,
		Handlers:       handlers,
		AuthFlow:       authFlow,
		InboundGit:     integrations.GitEvents,
		InboundLogging: integrations.ObsAlertsHandler("logging"),
		InboundMetrics: integrations.ObsAlertsHandler("metrics"),
	})

	srv := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	// Prometheus and the container probes get their own listener, deliberately not
	// the published one. /metrics used to sit on the public router: ~74KB of route
	// patterns, per-route traffic and runtime internals, to anyone who could reach
	// the API port. Scrape this from inside; publish only cfg.Server.Addr.
	adminSrv := httpapi.AdminServer(cfg.Server.MetricsAddr, metrics.Registry, db, logger)

	go func() {
		logger.Info("api listening", "addr", cfg.Server.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen", "err", err)
			stop()
		}
	}()

	go func() {
		logger.Info("api metrics listening", "addr", adminSrv.Addr)
		if err := adminSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			// Not fatal: losing observability should not take the API with it.
			logger.Error("metrics listen", "err", err)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	_ = adminSrv.Shutdown(shutdownCtx)
}
