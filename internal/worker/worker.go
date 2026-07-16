// Package worker registers and runs the River background-job processors: the event
// dispatcher plus notify, webhook, search-index, automation, git-ingest and obs-ingest.
package worker

import (
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/omni/bugtracker/internal/config"
	"github.com/omni/bugtracker/internal/events"
	"github.com/omni/bugtracker/internal/integrations"
	"github.com/omni/bugtracker/internal/platform"
	"github.com/omni/bugtracker/internal/service"
)

type Deps struct {
	DB       *pgxpool.Pool
	Cfg      *config.Config
	Logger   *slog.Logger
	Metrics  *platform.Metrics
	Store    service.Repository
	Adapters *integrations.Registry
}

// New builds the River client with every worker registered and queues configured.
func New(d Deps) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &eventWorker{d: d})
	river.AddWorker(workers, &notifyWorker{d: d})
	river.AddWorker(workers, &webhookWorker{d: d})
	river.AddWorker(workers, &indexWorker{d: d})
	river.AddWorker(workers, &automationWorker{d: d})
	river.AddWorker(workers, &gitIngestWorker{d: d})
	river.AddWorker(workers, &obsIngestWorker{d: d})
	river.AddWorker(workers, &autoArchiveWorker{d: d})

	q := d.Cfg.Worker.Queues
	queues := map[string]river.QueueConfig{
		"default":      {MaxWorkers: queueSize(q, "default", 10)},
		"webhooks":     {MaxWorkers: queueSize(q, "webhooks", 5)},
		"integrations": {MaxWorkers: queueSize(q, "integrations", 5)},
	}

	// Auto-archive: always register the daily job. Whether it archives anything is
	// decided at run time from the Settings value (DB, falling back to config), so an
	// admin can toggle it without restarting the worker. The job no-ops when disabled.
	periodic := []*river.PeriodicJob{
		river.NewPeriodicJob(
			river.PeriodicInterval(24*time.Hour),
			func() (river.JobArgs, *river.InsertOpts) { return events.AutoArchiveArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		),
	}

	return river.NewClient(riverpgxv5.New(d.DB), &river.Config{
		Queues:       queues,
		Workers:      workers,
		PeriodicJobs: periodic,
	})
}

func queueSize(q map[string]int, name string, def int) int {
	if v, ok := q[name]; ok && v > 0 {
		return v
	}
	return def
}
