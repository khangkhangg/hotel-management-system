// Package audit is the tamper-evident activity trail (spec 06). Handlers call Record inside the transaction
// that makes the change, so a change and its entry commit together or not at all.
package audit

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/ids"
)

// Who is the person acting. Role is always their real role; ViewAs is the role they are looking at the app as.
type Who struct {
	UserID     uuid.UUID
	OrgID      uuid.UUID
	PropertyID uuid.UUID // uuid.Nil when the request names no property
	Role       string
	ViewAs     string
}

type whoKey struct{}

func WithWho(ctx context.Context, w Who) context.Context { return context.WithValue(ctx, whoKey{}, w) }

func WhoFrom(ctx context.Context) Who { w, _ := ctx.Value(whoKey{}).(Who); return w }

// Entry is one thing that happened. Before and After may be anything that marshals to JSON; secrets are removed.
type Entry struct {
	Action      string
	TargetType  string
	TargetID    uuid.UUID
	TargetLabel string
	Before      any
	After       any
	Reason      string
}

var (
	ErrNoActor  = errors.New("audit: no signed-in person in the context")
	ErrNoAction = errors.New("audit: an entry needs an action and a target type")
)

// Record writes the entry for the person in ctx. It must run in the transaction of the change it describes.
func Record(ctx context.Context, tx pgx.Tx, e Entry) error {
	w := WhoFrom(ctx)
	if w.UserID == uuid.Nil || w.OrgID == uuid.Nil {
		return ErrNoActor
	}
	return write(ctx, tx, w.OrgID, &w.UserID, w.Role, w.ViewAs, w.PropertyID, e)
}

// RecordSystem writes an entry for work done by a job, with no person behind it.
func RecordSystem(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, job string, e Entry) error {
	return write(ctx, tx, orgID, nil, "system:"+job, "", uuid.Nil, e)
}

func write(ctx context.Context, tx pgx.Tx, org uuid.UUID, actor *uuid.UUID, role, viewAs string, property uuid.UUID, e Entry) error {
	if strings.TrimSpace(e.Action) == "" || strings.TrimSpace(e.TargetType) == "" {
		return ErrNoAction
	}
	before, err := redactedJSON(e.Before)
	if err != nil {
		return err
	}
	after, err := redactedJSON(e.After)
	if err != nil {
		return err
	}
	// One writer per organization at a time, so sequence numbers and hashes form a single line.
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, 7302))`, org.String()); err != nil {
		return err
	}
	var seq int64
	var prev []byte
	err = tx.QueryRow(ctx, `select seq, row_hash from audit_log where organization_id = $1 order by seq desc limit 1`, org).Scan(&seq, &prev)
	if errors.Is(err, pgx.ErrNoRows) {
		seq, prev, err = 0, genesis, nil
	}
	if err != nil {
		return err
	}
	info := httpx.Info(ctx)
	r := row{
		ID: ids.New(), Org: org, Seq: seq + 1, At: time.Now().UTC().Truncate(time.Microsecond),
		Actor: actor, Role: role, ViewAs: viewAs, Action: e.Action, TargetType: e.TargetType, TargetLabel: e.TargetLabel,
		Before: before, After: after, Reason: e.Reason, Device: info.UserAgent, IP: info.IP, Prev: prev,
	}
	if property != uuid.Nil {
		r.Property = &property
	}
	if e.TargetID != uuid.Nil {
		r.Target = &e.TargetID
	}
	if info.TraceID != uuid.Nil {
		r.Trace = &info.TraceID
	}
	r.Hash, err = r.hash()
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`insert into audit_log (id, organization_id, seq, occurred_at, actor_user_id, actor_role, view_as, property_id, action, target_type, target_id,
		   target_label, before, after, reason, device, ip, trace_id, prev_hash, row_hash)
		 values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
		r.ID, r.Org, r.Seq, r.At, r.Actor, r.Role, r.ViewAs, r.Property, r.Action, r.TargetType, r.Target, r.TargetLabel,
		nullJSON(r.Before), nullJSON(r.After), r.Reason, r.Device, r.IP, r.Trace, r.Prev, r.Hash)
	return err
}

func nullJSON(b []byte) any {
	if b == nil {
		return nil
	}
	return string(b)
}

// RecordForUser writes the entry as the person, once in each organization they belong to. It is for things that
// happen to a person rather than inside one organization: a sign-in, a failed sign-in, a password change.
// It switches the transaction between organizations and leaves none selected, so call it last.
func RecordForUser(ctx context.Context, tx pgx.Tx, userID uuid.UUID, e Entry) error {
	return recordForUser(ctx, tx, userID, nil, e)
}

// RecordAtOrg is RecordForUser for one organization, such as the one a person has just chosen.
func RecordAtOrg(ctx context.Context, tx pgx.Tx, userID, orgID uuid.UUID, e Entry) error {
	return recordForUser(ctx, tx, userID, &orgID, e)
}

func recordForUser(ctx context.Context, tx pgx.Tx, userID uuid.UUID, only *uuid.UUID, e Entry) error {
	if _, err := tx.Exec(ctx, `select set_config('app.user_id', $1, true)`, userID.String()); err != nil {
		return err
	}
	rows, err := tx.Query(ctx,
		`select distinct on (organization_id) organization_id, property_id, role from membership
		 where user_id = $1 and status = 'active' and ($2::uuid is null or organization_id = $2)
		 order by organization_id, property_id nulls first`, userID, only)
	if err != nil {
		return err
	}
	type place struct {
		org      uuid.UUID
		property *uuid.UUID
		role     string
	}
	var places []place
	for rows.Next() {
		var p place
		if err := rows.Scan(&p.org, &p.property, &p.role); err != nil {
			rows.Close()
			return err
		}
		places = append(places, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range places {
		if _, err := tx.Exec(ctx, `select set_config('app.org_id', $1, true)`, p.org.String()); err != nil {
			return err
		}
		property := uuid.Nil
		if p.property != nil {
			property = *p.property
		}
		if err := write(ctx, tx, p.org, &userID, p.role, "", property, e); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `select set_config('app.org_id', '', true)`)
	return err
}
