// Command migrate applies goose schema migrations. River's own queue tables are
// created separately by `river migrate-up` (see the Makefile `migrate` target).
package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

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

	pool, err := pgxpool.New(context.Background(), cfg.Database.DSN)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatalf("dialect: %v", err)
	}
	if err := goose.RunContext(context.Background(), cmd, db, *dir, flag.Args()[min(1, len(flag.Args())):]...); err != nil {
		log.Fatalf("goose %s: %v", cmd, err)
	}
	os.Exit(0)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
