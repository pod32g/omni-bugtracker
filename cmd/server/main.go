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
	logger := platform.NewLogger(cfg.Log)
	metrics := platform.NewMetrics()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := platform.NewDBPool(ctx, cfg.Database)
	if err != nil {
		logger.Error("db connect", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	// Redis is used for caching / rate limiting only.
	if _, err := platform.NewRedis(ctx, cfg.Redis); err != nil {
		logger.Warn("redis unavailable — cache disabled", "err", err)
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

	router := httpapi.NewRouter(httpapi.Deps{
		Cfg:      cfg,
		Logger:   logger,
		Metrics:  metrics,
		DB:       db,
		Verifier: verifier,
		Authn:    authn,
		Handlers: handlers,
	})

	srv := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	go func() {
		logger.Info("api listening", "addr", cfg.Server.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
