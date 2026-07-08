// Command worker runs the background job processors (River): notifications, outbound
// webhooks, search indexing, automation rules, git ingestion, and observability ingestion.
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

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

	if err := riverClient.Start(ctx); err != nil {
		logger.Error("worker start", "err", err)
		os.Exit(1)
	}
	logger.Info("workers started", "queues", cfg.Worker.Queues)

	<-ctx.Done()
	logger.Info("draining workers")
	_ = riverClient.Stop(context.Background())
	_ = events.Noop() // keep events import referenced for wiring parity
}
