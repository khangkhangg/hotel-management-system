// Package notify is the notification core (spec 06): one in-app notification per person and key, email for what
// is urgent, quiet hours for what can wait, escalation for what nobody opened, and a live stream.
package notify

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/ids"

	_ "time/tzdata" // quiet hours use the property's time zone, and a server may have no zone database
)

type Level string

const (
	Urgent       Level = "urgent"        // in-app and email, ignores quiet hours, escalates if nobody opens it
	ActionNeeded Level = "action_needed" // in-app, and email if the person chose it
	Info         Level = "info"          // in-app only
)

// Input describes one notification. It goes to ToUser, or to everyone with ToRole at PropertyID
// (owners work at every property, so they receive it for any property of the organization).
type Input struct {
	OrgID      uuid.UUID
	PropertyID uuid.UUID
	ToRole     access.Role
	ToUser     *uuid.UUID
	Level      Level
	Key        string // the same key for the same person updates their open notification and raises its count
	Title      string
	Body       string
	ActionURL  string
	Now        time.Time // zero means the current time
}

// Send adds the notification for each recipient, inside the caller's transaction (db.WithOrg for in.OrgID).
// Emails are not sent here; Deliver sends them, so a rolled-back change never sends one.
func Send(ctx context.Context, tx pgx.Tx, in Input) error {
	if in.Now.IsZero() {
		in.Now = time.Now()
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
		due, err := emailDue(ctx, tx, user, in.Level, in.Now, loc)
		if err != nil {
			return err
		}
		if err := insert(ctx, tx, user, in, due, 0, true); err != nil {
			return err
		}
	}
	return nil
}

func recipientsOf(ctx context.Context, tx pgx.Tx, in Input) ([]uuid.UUID, error) {
	if in.ToUser != nil {
		return []uuid.UUID{*in.ToUser}, nil
	}
	rows, err := tx.Query(ctx,
		`select distinct user_id from membership
		 where organization_id = $1 and status = 'active' and role = $2 and (property_id = $3 or property_id is null)`,
		in.OrgID, string(in.ToRole), in.PropertyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func zoneOf(ctx context.Context, tx pgx.Tx, property uuid.UUID) (*time.Location, error) {
	var name string
	if err := tx.QueryRow(ctx, `select time_zone from property where id = $1`, property).Scan(&name); err != nil {
		if err == pgx.ErrNoRows {
			return time.UTC, nil
		}
		return nil, err
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC, nil // a bad stored zone must not stop a notification
	}
	return loc, nil
}

// emailDue says when the email for a new notification should go out, or nil for no email.
func emailDue(ctx context.Context, tx pgx.Tx, user uuid.UUID, level Level, now time.Time, loc *time.Location) (*time.Time, error) {
	if level == Info {
		return nil, nil
	}
	p, err := preferencesOf(ctx, tx, user)
	if err != nil {
		return nil, err
	}
	switch level {
	case Urgent:
		if !p.EmailUrgent {
			return nil, nil
		}
		return &now, nil
	default:
		if !p.EmailActionNeeded {
			return nil, nil
		}
		due := afterQuietHours(now, loc, p.QuietFrom, p.QuietTo)
		return &due, nil
	}
}

// afterQuietHours returns now, or the end of the quiet window when now falls inside it. Times are "HH:MM" in loc.
func afterQuietHours(now time.Time, loc *time.Location, from, to *string) time.Time {
	if from == nil || to == nil || *from == *to {
		return now
	}
	f, okF := minutes(*from)
	t, okT := minutes(*to)
	if !okF || !okT {
		return now
	}
	local := now.In(loc)
	at := local.Hour()*60 + local.Minute()
	inside := (f < t && at >= f && at < t) || (f > t && (at >= f || at < t))
	if !inside {
		return now
	}
	end := time.Date(local.Year(), local.Month(), local.Day(), t/60, t%60, 0, 0, loc)
	if !end.After(local) {
		end = end.AddDate(0, 0, 1)
	}
	return end
}

func minutes(hhmm string) (int, bool) {
	var h, m int
	if _, err := fmt.Sscanf(hhmm, "%d:%d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// insert adds one person's notification. When one with the same key is already open it raises that one's
// count instead, unless bump is false, in which case the person keeps theirs as it is.
func insert(ctx context.Context, tx pgx.Tx, user uuid.UUID, in Input, due *time.Time, escalation int, bump bool) error {
	conflict := `do nothing`
	if bump {
		conflict = `do update set count = notification.count + 1, title = excluded.title, body = excluded.body,
		            action_url = excluded.action_url, level = excluded.level, updated_at = excluded.updated_at`
	}
	if _, err := tx.Exec(ctx,
		`insert into notification (id, organization_id, property_id, user_id, level, key, title, body, action_url, created_at, updated_at, email_due_at, escalation)
		 values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10, $11, $12)
		 on conflict (organization_id, property_id, user_id, key) where resolved_at is null `+conflict,
		ids.New(), in.OrgID, in.PropertyID, user, string(in.Level), in.Key, in.Title, in.Body, in.ActionURL, in.Now, due, escalation); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `select pg_notify('notifications', $1)`, user.String())
	return err
}

// Resolve closes every open notification with the key, for everyone who holds it.
func Resolve(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, key string, by uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`update notification set resolved_at = now(), resolved_by = $3, updated_at = now()
		 where organization_id = $1 and key = $2 and resolved_at is null`, orgID, key, by)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`select pg_notify('notifications', user_id::text) from notification where organization_id = $1 and key = $2 and resolved_by = $3`, orgID, key, by)
	return err
}

type Preferences struct {
	QuietFrom         *string
	QuietTo           *string
	EmailUrgent       bool
	EmailActionNeeded bool
}

// preferencesOf returns a person's choices, or the defaults (urgent by email, nothing else) when they made none.
func preferencesOf(ctx context.Context, tx pgx.Tx, user uuid.UUID) (Preferences, error) {
	p := Preferences{EmailUrgent: true}
	err := tx.QueryRow(ctx,
		`select to_char(quiet_from, 'HH24:MI'), to_char(quiet_to, 'HH24:MI'), email_urgent, email_action_needed
		 from notification_preference where user_id = $1`, user).Scan(&p.QuietFrom, &p.QuietTo, &p.EmailUrgent, &p.EmailActionNeeded)
	if err == pgx.ErrNoRows {
		return Preferences{EmailUrgent: true}, nil
	}
	return p, err
}
