// Package alerts watches a hotel's data, finds problems early, and offers the fix as one button (spec 09).
// The checks are plain rules. They never change anything on their own: staff apply the suggested action.
package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/ids"
	"hotelpms/api/internal/notify"
)

// Action is a suggested fix: what the button does, and the changes it will make, so the person can check first.
type Action struct {
	Kind    string         `json:"kind"` // assign_room, no_show, or open (go to a screen)
	Label   string         `json:"label"`
	Changes []string       `json:"changes,omitempty"`
	Params  map[string]any `json:"params,omitempty"`
}

// Finding is one problem a check found.
type Finding struct {
	SubjectType string
	Subject     uuid.UUID
	Severity    string // urgent, warning or info
	Text        string
	Facts       map[string]any
	Action      *Action
	Role        access.Role // who it is for, when the check cannot say in advance (the author of a tag, for instance)
}

// Reason is stored with the alert: the sentence and the numbers behind it.
type Reason struct {
	Text  string         `json:"text"`
	Facts map[string]any `json:"facts,omitempty"`
}

// scope is what a check knows about the property it runs for.
type scope struct {
	orgID, propertyID uuid.UUID
	now               time.Time
	loc               *time.Location
	today             time.Time // the hotel day
	hour              float64   // hours since midnight in the property's time zone
	currency          string
	th                map[string]float64
}

type check struct {
	kind     string
	role     access.Role
	defaults map[string]float64
	find     func(context.Context, pgx.Tx, scope) ([]Finding, error)
}

// Run evaluates every enabled check of every property. Alerts open, update and close; staff are notified once per alert.
func Run(ctx context.Context, pool *pgxpool.Pool, now time.Time) error {
	type pair struct{ org, property uuid.UUID }
	var pairs []pair
	if err := db.WithoutOrg(ctx, pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select organization_id, property_id from all_properties()`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p pair
			if err := rows.Scan(&p.org, &p.property); err != nil {
				return err
			}
			pairs = append(pairs, p)
		}
		return rows.Err()
	}); err != nil {
		return err
	}
	var failures []error
	for _, p := range pairs {
		if err := db.WithOrg(ctx, pool, p.org, func(tx pgx.Tx) error { return RunProperty(ctx, tx, p.org, p.property, now) }); err != nil {
			failures = append(failures, fmt.Errorf("property %s: %w", p.property, err))
		}
	}
	return errors.Join(failures...)
}

// RunSweeper runs the checks every interval until ctx ends.
func RunSweeper(ctx context.Context, pool *pgxpool.Pool, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := Run(ctx, pool, time.Now()); err != nil {
				log.Printf("alert checks: %v", err)
			}
		}
	}
}

func settings(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID, c check) (enabled bool, th map[string]float64, err error) {
	enabled, th = true, map[string]float64{}
	for k, v := range c.defaults {
		th[k] = v
	}
	var raw []byte
	err = tx.QueryRow(ctx, `select enabled, thresholds from check_setting where property_id = $1 and check_kind = $2`, propertyID, c.kind).Scan(&enabled, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, th, nil
	}
	if err != nil {
		return
	}
	var custom map[string]float64
	_ = json.Unmarshal(raw, &custom)
	for k, v := range custom {
		if _, known := c.defaults[k]; known {
			th[k] = v
		}
	}
	return
}

// RunProperty runs the checks for one property inside the caller's transaction (db.WithOrg for orgID).
func RunProperty(ctx context.Context, tx pgx.Tx, orgID, propertyID uuid.UUID, now time.Time) error {
	today, currency, err := booking.HotelToday(ctx, tx, orgID, propertyID, now)
	if err != nil {
		return err
	}
	var zone string
	if err := tx.QueryRow(ctx, `select time_zone from property where id = $1`, propertyID).Scan(&zone); err != nil {
		return err
	}
	loc, lerr := time.LoadLocation(zone)
	if lerr != nil {
		loc = time.UTC
	}
	local := now.In(loc)
	base := scope{orgID: orgID, propertyID: propertyID, now: now, loc: loc, today: today, hour: float64(local.Hour()) + float64(local.Minute())/60, currency: currency}

	// Snoozed alerts come back once their time is up and the problem is still there.
	if _, err := tx.Exec(ctx, `update alert set status = 'open', snoozed_until = null where property_id = $1 and status = 'snoozed' and snoozed_until <= $2`, propertyID, now); err != nil {
		return err
	}
	for _, c := range registry {
		enabled, th, err := settings(ctx, tx, propertyID, c)
		if err != nil {
			return err
		}
		if !enabled {
			continue // turning a check off already closed its alerts
		}
		sc := base
		sc.th = th
		found, err := c.find(ctx, tx, sc)
		if err != nil {
			return fmt.Errorf("%s: %w", c.kind, err)
		}
		if err := reconcile(ctx, tx, sc, c, found); err != nil {
			return fmt.Errorf("%s: %w", c.kind, err)
		}
	}
	_, err = tx.Exec(ctx, `insert into alert_run (property_id, organization_id, ran_at) values ($1, $2, $3) on conflict (property_id) do update set ran_at = excluded.ran_at`, propertyID, orgID, now)
	return err
}

// reconcile makes the open alerts of one check match what it found now.
func reconcile(ctx context.Context, tx pgx.Tx, sc scope, c check, found []Finding) error {
	still := map[uuid.UUID]bool{}
	for _, f := range found {
		role := c.role
		if f.Role != "" {
			role = f.Role
		}
		still[f.Subject] = true
		reason, _ := json.Marshal(Reason{Text: f.Text, Facts: f.Facts})
		var action []byte
		if f.Action != nil {
			action, _ = json.Marshal(f.Action)
		}
		// A problem that is already open or snoozed is updated, never duplicated.
		tag, err := tx.Exec(ctx, `update alert set reason = $4, suggested_action = $5, severity = $6 where property_id = $1 and check_kind = $2 and subject_id = $3 and status in ('open', 'snoozed')`,
			sc.propertyID, c.kind, f.Subject, reason, nullJSON(action), f.Severity)
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			continue
		}
		// Someone dismissed the same cause in the last 24 hours: it stays away.
		var dismissed bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from alert where property_id = $1 and check_kind = $2 and subject_id = $3 and status = 'dismissed' and closed_at > $4)`,
			sc.propertyID, c.kind, f.Subject, sc.now.Add(-24*time.Hour)).Scan(&dismissed); err != nil {
			return err
		}
		if dismissed {
			continue
		}
		id := ids.New()
		if _, err := tx.Exec(ctx,
			`insert into alert (id, organization_id, property_id, check_kind, subject_type, subject_id, severity, reason, suggested_action, target_role, opened_at)
			 values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			id, sc.orgID, sc.propertyID, c.kind, f.SubjectType, f.Subject, f.Severity, reason, nullJSON(action), string(role), sc.now); err != nil {
			return err
		}
		level := notify.ActionNeeded
		switch f.Severity {
		case "urgent":
			level = notify.Urgent
		case "info":
			level = notify.Info
		}
		if err := notify.Send(ctx, tx, notify.Input{OrgID: sc.orgID, PropertyID: sc.propertyID, ToRole: role, Level: level, Key: "alert:" + id.String(), Title: f.Text, ActionURL: "/", Now: sc.now}); err != nil {
			return err
		}
		if err := audit.RecordSystem(ctx, tx, sc.orgID, "alerts", audit.Entry{Action: "alert.opened", TargetType: "alert", TargetID: id, TargetLabel: c.kind, After: map[string]any{"text": f.Text}}); err != nil {
			return err
		}
	}
	// Alerts whose cause is gone close by themselves.
	rows, err := tx.Query(ctx, `select id, subject_id from alert where property_id = $1 and check_kind = $2 and status in ('open', 'snoozed')`, sc.propertyID, c.kind)
	if err != nil {
		return err
	}
	var gone []uuid.UUID
	for rows.Next() {
		var id, subject uuid.UUID
		if err := rows.Scan(&id, &subject); err != nil {
			rows.Close()
			return err
		}
		if !still[subject] {
			gone = append(gone, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range gone {
		if err := closeAlert(ctx, tx, sc.orgID, id, "closed", "resolved", nil, sc.now); err != nil {
			return err
		}
	}
	return nil
}

func nullJSON(b []byte) any {
	if b == nil {
		return nil
	}
	return string(b)
}

// closeAlert ends an alert with the given status. A person closing it is recorded; a self-closing alert has no person.
func closeAlert(ctx context.Context, tx pgx.Tx, orgID, id uuid.UUID, status, why string, by *uuid.UUID, now time.Time) error {
	if _, err := tx.Exec(ctx, `update alert set status = $2, close_reason = $3, closed_by = $4, closed_at = $5, snoozed_until = null where id = $1`, id, status, why, by, now); err != nil {
		return err
	}
	return notify.ResolveSystem(ctx, tx, orgID, "alert:"+id.String())
}
