// Command migrate applies River's job-queue migrations and goose schema migrations
// against the same Postgres database, so a single `migrate up` fully provisions the DB.
package main

import (
	"context"
	"flag"
	"fmt"
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
	var extra []string
	if args := flag.Args(); len(args) > 1 {
		extra = args[1:]
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

	if err := run(ctx, pool, *dir, cmd, extra); err != nil {
		log.Fatalf("migrate %s: %v", cmd, err)
	}
	log.Printf("migrate %s: done", cmd)
}

// movesForward reports whether cmd applies migrations rather than reverting or
// merely reporting on them. River's tables are provisioned for exactly these.
func movesForward(cmd string) bool {
	switch cmd {
	case "up", "up-to", "up-by-one":
		return true
	}
	return false
}

// run provisions the database.
//
// River's job-queue tables go first. Schema migrations may legitimately
// reference them — 00028 clears stale search_index jobs — and on a database
// that has never been provisioned, goose would otherwise die on a table River
// has not created yet. That failure was invisible to anyone with an existing
// database, because their River tables predate the migration that needs them;
// it only ever hit a genuinely fresh one, which is to say new contributors and
// from-scratch CI.
//
// Nothing in the goose set is a prerequisite for River's own tables, so this
// ordering is safe: River's migrations touch only their own schema.
func run(ctx context.Context, pool *pgxpool.Pool, dir, cmd string, extra []string) error {
	if movesForward(cmd) {
		migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
		if err != nil {
			return fmt.Errorf("river migrator: %w", err)
		}
		if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
			return fmt.Errorf("river migrate up: %w", err)
		}
	}

	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("dialect: %w", err)
	}
	if err := goose.RunContext(ctx, cmd, db, dir, extra...); err != nil {
		return fmt.Errorf("goose %s: %w", cmd, err)
	}
	return nil
}
