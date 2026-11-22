package notify

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/mail"
)

// orgsDue names the organizations that have an email to send or an escalation to make.
func orgsDue(ctx context.Context, pool *pgxpool.Pool, now time.Time) ([]uuid.UUID, error) {
	var out []uuid.UUID
	err := db.WithoutOrg(ctx, pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select notification_orgs_due($1)`, now)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return err
			}
			out = append(out, id)
		}
		return rows.Err()
	})
	return out, err
}

// Deliver sends the emails that have come due and marks them sent, so running it again sends nothing twice.
// One failing email does not stop the others; it stays due and is tried on the next run.
func Deliver(ctx context.Context, pool *pgxpool.Pool, sender mail.Sender, now time.Time) error {
	orgs, err := orgsDue(ctx, pool, now)
	if err != nil {
		return err
	}
	var failures []error
	for _, org := range orgs {
		err := db.WithOrg(ctx, pool, org, func(tx pgx.Tx) error {
			type due struct {
				id                          uuid.UUID
				to, title, body, actionPath string
			}
			rows, err := tx.Query(ctx,
				`select n.id, u.email, n.title, n.body, n.action_url from notification n join app_user u on u.id = n.user_id
				 where n.organization_id = $1 and n.resolved_at is null and n.email_due_at <= $2 and n.email_sent_at is null order by n.created_at`, org, now)
			if err != nil {
				return err
			}
			var list []due
			for rows.Next() {
				var d due
				if err := rows.Scan(&d.id, &d.to, &d.title, &d.body, &d.actionPath); err != nil {
					rows.Close()
					return err
				}
				list = append(list, d)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
			for _, d := range list {
				text := d.body
				if d.actionPath != "" {
					text += "\n\nOpen: " + d.actionPath
				}
				if err := sender.Send(ctx, mail.Message{To: d.to, Subject: d.title, Text: text}); err != nil {
					failures = append(failures, err)
					continue
				}
				if _, err := tx.Exec(ctx, `update notification set email_sent_at = $2 where id = $1`, d.id, now); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// Escalate tells managers about urgent notifications nobody has opened after 10 minutes, and owners after 30.
// Opening or resolving a notification stops it. Running it again changes nothing that is already done.
func Escalate(ctx context.Context, pool *pgxpool.Pool, now time.Time) error {
	orgs, err := orgsDue(ctx, pool, now)
	if err != nil {
		return err
	}
	steps := []struct {
		after    time.Duration
		from, to int
		role     access.Role
	}{{10 * time.Minute, 0, 1, access.Manager}, {30 * time.Minute, 1, 2, access.Owner}}
	var failures []error
	for _, org := range orgs {
		err := db.WithOrg(ctx, pool, org, func(tx pgx.Tx) error {
			for _, s := range steps {
				if err := escalateStep(ctx, tx, org, now, s.after, s.from, s.to, s.role); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func escalateStep(ctx context.Context, tx pgx.Tx, org uuid.UUID, now time.Time, after time.Duration, from, to int, role access.Role) error {
	rows, err := tx.Query(ctx,
		`select n.id, n.property_id, n.key, n.title, n.body, n.action_url from notification n
		 where n.organization_id = $1 and n.level = 'urgent' and n.resolved_at is null and n.opened_at is null
		   and n.escalation = $2 and n.created_at <= $3
		   and not exists (select 1 from notification o where o.organization_id = n.organization_id and o.key = n.key
		                   and o.resolved_at is null and o.opened_at is not null)`, org, from, now.Add(-after))
	if err != nil {
		return err
	}
	type source struct {
		id                    uuid.UUID
		property              *uuid.UUID
		key, title, body, url string
	}
	var sources []source
	for rows.Next() {
		var s source
		if err := rows.Scan(&s.id, &s.property, &s.key, &s.title, &s.body, &s.url); err != nil {
			rows.Close()
			return err
		}
		sources = append(sources, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, s := range sources {
		in := Input{OrgID: org, ToRole: role, Level: Urgent, Key: s.key, Title: "Needs attention: " + s.title, Body: s.body, ActionURL: s.url, Now: now}
		if s.property != nil {
			in.PropertyID = *s.property
		}
		recipients, err := recipientsOf(ctx, tx, in)
		if err != nil {
			return err
		}
		loc, err := zoneOf(ctx, tx, in.PropertyID)
		if err != nil {
			return err
		}
		for _, user := range recipients {
			due, err := emailDue(ctx, tx, user, Urgent, now, loc)
			if err != nil {
				return err
			}
			if err := insert(ctx, tx, user, in, due, 2, false); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `update notification set escalation = $2 where id = $1`, s.id, to); err != nil {
			return err
		}
	}
	return nil
}

// Sweep does the periodic work: escalate, then send what is due.
func Sweep(ctx context.Context, pool *pgxpool.Pool, sender mail.Sender, now time.Time) error {
	return errors.Join(Escalate(ctx, pool, now), Deliver(ctx, pool, sender, now))
}

// RunSweeper calls Sweep every interval until ctx ends.
func RunSweeper(ctx context.Context, pool *pgxpool.Pool, sender mail.Sender, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := Sweep(ctx, pool, sender, time.Now()); err != nil {
				log.Printf("notification sweep: %v", err)
			}
		}
	}
}
