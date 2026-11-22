// Package db opens the connection pool and runs work inside transactions that carry the organization.
package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// WithOrg runs fn in a transaction whose row-level security policies see only orgID.
// The setting is local to the transaction, so it cannot leak to the next user of the connection.
func WithOrg(ctx context.Context, pool *pgxpool.Pool, orgID uuid.UUID, fn func(pgx.Tx) error) error {
	return run(ctx, pool, &orgID, fn)
}

// WithoutOrg runs fn with no organization. Policy-protected tables return no rows.
// It is for the person-level tables (users, sessions) and for creating an organization.
func WithoutOrg(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	return run(ctx, pool, nil, fn)
}

func run(ctx context.Context, pool *pgxpool.Pool, orgID *uuid.UUID, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	if orgID != nil {
		if _, err := tx.Exec(ctx, `select set_config('app.org_id', $1, true)`, orgID.String()); err != nil {
			return err
		}
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
