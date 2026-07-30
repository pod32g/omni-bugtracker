package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This test provisions a real, empty database, because that is the only place
// the bug it guards against can appear. Goose migration 00028 deletes stale
// search_index rows from river_job, and `migrate` used to run goose before
// River's own migrations — so `migrate up` failed with 42P01 on any database
// that had never been provisioned. Every existing database already had River's
// tables from an earlier run, which is why this went unnoticed: it broke only
// new-contributor setup and from-scratch CI, and under docker compose the
// migrate service is `restart: on-failure`, so it retried the same failure
// forever instead of failing loudly.
//
// Opt-in: OMNI_BT_TEST_DSN points at any reachable database on the target
// server. The test creates its own uniquely-named scratch database, migrates
// that, and drops it — it never touches the database in the DSN.
//
//	OMNI_BT_TEST_DSN="postgres://omni:omni@localhost:15499/postgres?sslmode=disable" \
//	  go test ./cmd/migrate/ -run Integration
func TestIntegrationMigrateProvisionsEmptyDatabase(t *testing.T) {
	adminDSN := os.Getenv("OMNI_BT_TEST_DSN")
	if adminDSN == "" {
		t.Skip("set OMNI_BT_TEST_DSN to run the migration integration test")
	}
	ctx := context.Background()

	name := "obt_migrate_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	admin, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	// Registered before the DROP below so it runs after it: cleanups are LIFO,
	// and a `defer` here would close the connection first and strand the
	// scratch database.
	t.Cleanup(func() { _ = admin.Close(ctx) })

	// CREATE/DROP DATABASE cannot run inside a transaction, and the name is
	// generated here rather than taken from input, so interpolating it is safe.
	if _, err := admin.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", name)); err != nil {
		t.Fatalf("create scratch database: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", name)); err != nil {
			t.Errorf("drop scratch database %s: %v", name, err)
		}
	})

	scratchDSN, err := withDatabase(adminDSN, name)
	if err != nil {
		t.Fatalf("build scratch dsn: %v", err)
	}
	pool, err := pgxpool.New(ctx, scratchDSN)
	if err != nil {
		t.Fatalf("connect to scratch: %v", err)
	}
	defer pool.Close()

	// The whole point: one command, from empty to fully provisioned.
	if err := run(ctx, pool, "../../db/migrations", "up", nil); err != nil {
		t.Fatalf("migrate up on an empty database: %v", err)
	}

	for _, table := range []string{"river_job", "issues", "comments", "activity"} {
		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT to_regclass($1) IS NOT NULL`, "public."+table).Scan(&exists); err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s missing after migrate up", table)
		}
	}

	// Goose must have run the whole set, not stopped short of the migration
	// that needs River.
	var version int64
	if err := pool.QueryRow(ctx,
		`SELECT max(version_id) FROM goose_db_version`).Scan(&version); err != nil {
		t.Fatalf("read goose version: %v", err)
	}
	if version < 28 {
		t.Errorf("goose stopped at version %d, before the migration that references river_job", version)
	}

	// Re-running must be a no-op rather than an error, since the compose
	// migrate service and any redeploy will do exactly that.
	if err := run(ctx, pool, "../../db/migrations", "up", nil); err != nil {
		t.Errorf("second migrate up: %v", err)
	}
}

// withDatabase swaps the database name in a Postgres URL, keeping credentials,
// host and options intact.
func withDatabase(dsn, name string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	u.Path = "/" + name
	return u.String(), nil
}
