// Package db opens the connection pool and runs work inside transactions that carry who is asking.
package db

import (
	"context"
	"encoding/hex"

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
	return run(ctx, pool, &orgID, nil, nil, fn)
}

// WithoutOrg runs fn with no organization. Policy-protected tables return no rows.
// It is for the person-level tables (users, sessions) and for creating an organization.
func WithoutOrg(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	return run(ctx, pool, nil, nil, nil, fn)
}

// WithUser runs fn as a signed-in person who has not chosen an organization yet. It can read that
// person's own memberships and the organizations they belong to, across organizations, and nothing else.
func WithUser(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, fn func(pgx.Tx) error) error {
	return run(ctx, pool, nil, &userID, nil, fn)
}

// WithOrgUser is WithOrg plus the person's cross-organization read access.
func WithOrgUser(ctx context.Context, pool *pgxpool.Pool, orgID, userID uuid.UUID, fn func(pgx.Tx) error) error {
	return run(ctx, pool, &orgID, &userID, nil, fn)
}

// WithInvite runs fn for someone holding an invite link. Only the invite whose token hash is given is visible.
// A transaction that goes on to write must then set the organization itself (see tenant.CreateOrganization).
func WithInvite(ctx context.Context, pool *pgxpool.Pool, tokenHash []byte, fn func(pgx.Tx) error) error {
	return run(ctx, pool, nil, nil, tokenHash, fn)
}

func run(ctx context.Context, pool *pgxpool.Pool, orgID, userID *uuid.UUID, inviteHash []byte, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	for name, id := range map[string]*uuid.UUID{"app.org_id": orgID, "app.user_id": userID} {
		if id == nil {
			continue
		}
		if _, err := tx.Exec(ctx, `select set_config($1, $2, true)`, name, id.String()); err != nil {
			return err
		}
	}
	if inviteHash != nil {
		if _, err := tx.Exec(ctx, `select set_config('app.invite_hash', $1, true)`, hex.EncodeToString(inviteHash)); err != nil {
			return err
		}
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
