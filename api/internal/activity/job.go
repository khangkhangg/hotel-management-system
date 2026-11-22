package activity

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/notify"
)

// VerifyAll checks the hash chain of every organization. A broken chain sends the owners an urgent
// notification; a second night with the same break raises that notification's count instead of adding another.
func VerifyAll(ctx context.Context, pool *pgxpool.Pool, now time.Time) error {
	var orgs []uuid.UUID
	err := db.WithoutOrg(ctx, pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select audit_organizations()`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return err
			}
			orgs = append(orgs, id)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	var failures []error
	for _, org := range orgs {
		err := db.WithOrg(ctx, pool, org, func(tx pgx.Tx) error {
			broken, err := audit.VerifyChain(ctx, tx, org)
			if err != nil || broken == nil {
				return err
			}
			return notify.Send(ctx, tx, notify.Input{
				OrgID: org, ToRole: access.Owner, Level: notify.Urgent, Key: "trail-broken:" + org.String(),
				Title:     "The activity trail was changed after the fact",
				Body:      "An entry in the activity trail no longer matches its fingerprint, so it was edited or removed outside the app. Ask whoever runs the database to investigate.",
				ActionURL: "/activity", Now: now,
			})
		})
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// NextRun is the next 03:00 UTC after now.
func NextRun(now time.Time) time.Time {
	u := now.UTC()
	next := time.Date(u.Year(), u.Month(), u.Day(), 3, 0, 0, 0, time.UTC)
	if !next.After(u) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// RunNightly calls VerifyAll at 03:00 UTC each day until ctx ends.
func RunNightly(ctx context.Context, pool *pgxpool.Pool) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(NextRun(time.Now()))):
			if err := VerifyAll(ctx, pool, time.Now()); err != nil {
				log.Printf("nightly activity trail check: %v", err)
			}
		}
	}
}
