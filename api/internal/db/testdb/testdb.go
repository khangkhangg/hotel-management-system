// Package testdb gives each test its own freshly migrated PostgreSQL database.
// It reads PGHOST, PGPORT, PGUSER and PGPASSWORD (a superuser) from the environment.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/db"
)

const rolePassword = "test-only-password" // roles exist only in the local development cluster

func adminURL(database string) string {
	host, port := env("PGHOST", "127.0.0.1"), env("PGPORT", "5432")
	u := url.URL{Scheme: "postgres", User: url.UserPassword(env("PGUSER", "postgres"), os.Getenv("PGPASSWORD")), Host: host + ":" + port, Path: "/" + database}
	return u.String()
}

func roleURL(role, database string) string {
	host, port := env("PGHOST", "127.0.0.1"), env("PGPORT", "5432")
	u := url.URL{Scheme: "postgres", User: url.UserPassword(role, rolePassword), Host: host + ":" + port, Path: "/" + database}
	return u.String()
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// New creates a database, migrates it as "migrator", and returns a pool that connects as "app".
// The database is dropped when the test ends.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, cleanup, err := Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	return pool
}

// Create makes a fresh migrated database, for tests and for the end-to-end server. Call cleanup to drop it.
func Create(ctx context.Context) (pool *pgxpool.Pool, cleanup func(), err error) {
	admin, err := pgx.Connect(ctx, adminURL("postgres"))
	if err != nil {
		return nil, nil, fmt.Errorf("cannot connect to PostgreSQL (set PGHOST, PGPORT, PGUSER, PGPASSWORD): %w", err)
	}
	defer admin.Close(ctx)

	// Test packages run in parallel, so role creation is serialised with an advisory lock.
	if _, err := admin.Exec(ctx, `select pg_advisory_lock(7301)`); err != nil {
		return nil, nil, err
	}
	for _, role := range []string{"migrator", "app"} {
		_, err := admin.Exec(ctx, fmt.Sprintf(`do $$ begin
			if not exists (select from pg_roles where rolname = '%[1]s') then
				create role %[1]s login password '%[2]s';
			end if; end $$`, role, rolePassword))
		if err != nil {
			return nil, nil, err
		}
	}
	if _, err := admin.Exec(ctx, `select pg_advisory_unlock(7301)`); err != nil {
		return nil, nil, err
	}

	var suffix [6]byte
	_, _ = rand.Read(suffix[:])
	name := "hotel_test_" + hex.EncodeToString(suffix[:])
	if _, err := admin.Exec(ctx, fmt.Sprintf(`create database %s owner migrator`, name)); err != nil {
		return nil, nil, err
	}
	drop := func() {
		if c, err := pgx.Connect(context.Background(), adminURL("postgres")); err == nil {
			_, _ = c.Exec(context.Background(), fmt.Sprintf(`drop database if exists %s with (force)`, name))
			c.Close(context.Background())
		}
	}
	fail := func(err error) (*pgxpool.Pool, func(), error) { drop(); return nil, nil, err }

	if err := db.Migrate(ctx, roleURL("migrator", name)); err != nil {
		return fail(fmt.Errorf("migrate: %w", err))
	}
	cfg, err := pgxpool.ParseConfig(roleURL("app", name))
	if err != nil {
		return fail(err)
	}
	cfg.MaxConns = 8
	pool, err = pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fail(err)
	}
	return pool, func() { pool.Close(); drop() }, nil
}
