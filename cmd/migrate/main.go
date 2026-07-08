// Command migrate applies goose schema migrations and River's job-queue migrations
// against the same Postgres database, so a single `migrate up` fully provisions the DB.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/omni/bugtracker/internal/config"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	dir := flag.String("dir", "db/migrations", "migrations directory")
	flag.Parse()
	cmd := flag.Arg(0)
	if cmd == "" {
		cmd = "up"
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.Database.DSN)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	// 1) Schema migrations (goose).
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatalf("dialect: %v", err)
	}
	var extra []string
	if args := flag.Args(); len(args) > 1 {
		extra = args[1:]
	}
	if err := goose.RunContext(ctx, cmd, db, *dir, extra...); err != nil {
		log.Fatalf("goose %s: %v", cmd, err)
	}

	// 2) River job-queue migrations (only on `up`) — creates river_job et al.
	if cmd == "up" {
		migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
		if err != nil {
			log.Fatalf("river migrator: %v", err)
		}
		if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
			log.Fatalf("river migrate up: %v", err)
		}
	}
	log.Printf("migrate %s: done", cmd)
}
