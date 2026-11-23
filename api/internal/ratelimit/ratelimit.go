// Package ratelimit counts requests per key in the database, so every server shares one limit.
package ratelimit

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/db"
)

// Allow says whether the key may do one more thing now, and counts it when it may. At most max hits fit in the window.
// When the database cannot be reached it allows the request, because a limit must not stop a guest from booking.
func Allow(ctx context.Context, pool *pgxpool.Pool, key string, now time.Time, max int, window time.Duration) bool {
	allowed := true
	err := db.WithoutOrg(ctx, pool, func(tx pgx.Tx) error {
		// The lock makes two requests for one key count one after the other, on any server.
		if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1))`, key); err != nil {
			return err
		}
		// Old hits of this key go, and so does anything a day old, so addresses that never return leave no rows.
		if _, err := tx.Exec(ctx, `delete from rate_hit where (key = $1 and at <= $2) or at <= $3`, key, now.Add(-window), now.Add(-24*time.Hour)); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `select count(*) from rate_hit where key = $1`, key).Scan(&n); err != nil {
			return err
		}
		if n >= max {
			allowed = false
			return nil
		}
		_, err := tx.Exec(ctx, `insert into rate_hit (key, at) values ($1, $2)`, key, now)
		return err
	})
	return err != nil || allowed
}
