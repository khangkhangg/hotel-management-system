package staff_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/db"
)

type entry struct {
	Action, Role, TargetType, Label, Before, After string
	Actor                                          *uuid.UUID
	Target                                         *uuid.UUID
}

func (w *world) trail(org uuid.UUID) []entry {
	w.t.Helper()
	var out []entry
	must(w.t, db.WithOrg(ctx, w.pool, org, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select action, actor_role, target_type, target_label, coalesce(before::text, ''), coalesce(after::text, ''), actor_user_id, target_id from audit_log order by seq`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e entry
			if err := rows.Scan(&e.Action, &e.Role, &e.TargetType, &e.Label, &e.Before, &e.After, &e.Actor, &e.Target); err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	}))
	return out
}

func (w *world) only(org uuid.UUID, action string) entry {
	w.t.Helper()
	var hit []entry
	for _, e := range w.trail(org) {
		if e.Action == action {
			hit = append(hit, e)
		}
	}
	if len(hit) != 1 {
		w.t.Fatalf("%d %q entries, want 1: %+v", len(hit), action, w.trail(org))
	}
	return hit[0]
}

func TestInviteIsRecordedWithTheInviter(t *testing.T) {
	w := newWorld(t)
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	w.invite(mgr, ptr(w.a1), "new@hotel.com", "front_desk").want(t, 201)
	e := w.only(w.orgA, "user.invited")
	if e.Actor == nil || *e.Actor != mgr.id || e.Role != "manager" || e.Label != "new@hotel.com" || !strings.Contains(e.After, "front_desk") {
		t.Fatalf("%+v", e)
	}
	if strings.Contains(e.After, "token") {
		t.Fatalf("the invite token reached the trail: %s", e.After)
	}
}

func TestJoiningIsRecordedAsTheNewPerson(t *testing.T) {
	w := newWorld(t)
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	w.invite(mgr, ptr(w.a1), "new@hotel.com", "front_desk").want(t, 201)
	w.accept(w.lastLinkToken(), "New Person", goodPassword).want(t, 200)
	e := w.only(w.orgA, "user.joined")
	if e.Actor == nil || e.Role != "front_desk" || e.Label != "New Person" || e.Target == nil || *e.Target != *e.Actor {
		t.Fatalf("%+v", e)
	}
}

func TestRoleChangeDeactivationAndSignInResetAreRecorded(t *testing.T) {
	w := newWorld(t)
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	desk := w.member("lan@hotel.com", w.orgA, ptr(w.a1), "front_desk")
	id := "/api/staff/" + desk.id.String()

	w.do(mgr, "PATCH", id, ptr(w.a1), `{"role":"revenue"}`).want(t, 204)
	if e := w.only(w.orgA, "user.role_changed"); !strings.Contains(e.Before, "front_desk") || !strings.Contains(e.After, "revenue") || e.Target == nil || *e.Target != desk.id {
		t.Fatalf("%+v", e)
	}
	w.do(mgr, "POST", id+"/reset-sign-in", ptr(w.a1), "").want(t, 204)
	w.only(w.orgA, "user.signin_reset")
	w.do(mgr, "POST", id+"/deactivate", ptr(w.a1), "").want(t, 204)
	w.only(w.orgA, "user.deactivated")
	w.do(mgr, "POST", id+"/reactivate", ptr(w.a1), "").want(t, 204)
	w.only(w.orgA, "user.reactivated")
}

func TestPasswordResetIsRecordedInEveryOrganizationThePersonBelongsTo(t *testing.T) {
	w := newWorld(t)
	p := w.member("lan@hotel.com", w.orgA, ptr(w.a1), "front_desk")
	w.join(p, w.orgB, ptr(w.b1), "housekeeping")
	w.do(nil, "POST", "/api/password-reset", nil, `{"email":"lan@hotel.com"}`).want(t, 204)
	w.do(nil, "POST", "/api/password-reset/"+w.lastLinkToken(), nil, `{"password":"a brand new passphrase"}`).want(t, 204)
	for _, org := range []uuid.UUID{w.orgA, w.orgB} {
		e := w.only(org, "password.changed")
		if e.Actor == nil || *e.Actor != p.id || strings.Contains(e.After+e.Before, "passphrase") {
			t.Fatalf("%+v", e)
		}
	}
}

func TestARefusedActionLeavesNoEntry(t *testing.T) {
	w := newWorld(t)
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	w.do(mgr, "PATCH", "/api/staff/"+uuid.New().String(), ptr(w.a1), `{"role":"revenue"}`).want(t, 404)
	w.invite(mgr, ptr(w.a1), "not-an-email", "front_desk").want(t, 400)
	if n := len(w.trail(w.orgA)); n != 0 {
		t.Fatalf("%d entries after refused actions: %+v", n, w.trail(w.orgA))
	}
}
