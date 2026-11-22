package staff_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/account"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/session"
)

type row struct {
	ID, Name, Email, Role, PropertyID, PropertyName, Status string
	LastSignInAt                                            *string
}

func (w *world) list(p *person, property *uuid.UUID) map[string]row {
	w.t.Helper()
	var rows []row
	jsonOf(w.t, w.do(p, "GET", "/api/staff", property, "").want(w.t, 200), &rows)
	out := map[string]row{}
	for _, r := range rows {
		out[r.Email] = r
	}
	if len(out) != len(rows) {
		w.t.Fatalf("the list repeats an email: %+v", rows)
	}
	return out
}

func (w *world) setMembershipStatus(org uuid.UUID, user uuid.UUID, status string) {
	w.t.Helper()
	must(w.t, db.WithOrg(ctx, w.pool, org, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `update membership set status = $2 where user_id = $1`, user, status)
		return e
	}))
}

func TestStaffListShowsInvitedActiveAndDeactivated(t *testing.T) {
	w := newWorld(t)
	owner := w.member("linh@hotel.com", w.orgA, nil, "owner")
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	gone := w.member("lan@hotel.com", w.orgA, ptr(w.a1), "front_desk")
	w.member("minh@hotel.com", w.orgA, ptr(w.a2), "revenue")
	w.member("outsider@hotel.com", w.orgB, ptr(w.b1), "front_desk")
	w.setMembershipStatus(w.orgA, gone.id, "deactivated")
	at := time.Date(2026, 11, 22, 8, 30, 0, 0, time.UTC)
	must(t, db.WithoutOrg(ctx, w.pool, func(tx pgx.Tx) error { return account.TouchSignIn(ctx, tx, mgr.id, at) }))
	w.invite(mgr, ptr(w.a1), "new@hotel.com", "housekeeping").want(t, 201)

	got := w.list(owner, nil)
	if len(got) != 5 {
		t.Fatalf("%d rows, want 5 (owner, manager, deactivated, a2 member, pending invite): %+v", len(got), got)
	}
	want := map[string][3]string{ // email -> role, status, property name
		"linh@hotel.com":   {"owner", "active", "All properties"},
		"daniel@hotel.com": {"manager", "active", "a1"},
		"lan@hotel.com":    {"front_desk", "deactivated", "a1"},
		"minh@hotel.com":   {"revenue", "active", "a2"},
		"new@hotel.com":    {"housekeeping", "invited", "a1"},
	}
	for email, w3 := range want {
		r := got[email]
		if r.Role != w3[0] || r.Status != w3[1] || r.PropertyName != w3[2] {
			t.Errorf("%s: %+v, want %v", email, r, w3)
		}
	}
	if d := got["daniel@hotel.com"]; d.LastSignInAt == nil || *d.LastSignInAt != "2026-11-22T08:30:00Z" || d.Name != "Test Person" || d.ID != mgr.id.String() {
		t.Errorf("manager row: %+v", d)
	}
	if got["new@hotel.com"].LastSignInAt != nil {
		t.Error("an invited person has a last sign-in time")
	}
}

func TestAManagerSeesOnlyTheirPropertyAndNoOwners(t *testing.T) {
	w := newWorld(t)
	w.member("linh@hotel.com", w.orgA, nil, "owner")
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	w.member("lan@hotel.com", w.orgA, ptr(w.a1), "front_desk")
	w.member("minh@hotel.com", w.orgA, ptr(w.a2), "revenue")
	w.invite(mgr, ptr(w.a1), "new@hotel.com", "housekeeping").want(t, 201)
	got := w.list(mgr, ptr(w.a1))
	if len(got) != 3 || got["daniel@hotel.com"].Email == "" || got["lan@hotel.com"].Email == "" || got["new@hotel.com"].Email == "" {
		t.Fatalf("%+v", got)
	}
}

func TestAnOwnerWithAPropertySeesThatPropertyPlusOwners(t *testing.T) {
	w := newWorld(t)
	owner := w.member("linh@hotel.com", w.orgA, nil, "owner")
	w.member("lan@hotel.com", w.orgA, ptr(w.a1), "front_desk")
	w.member("minh@hotel.com", w.orgA, ptr(w.a2), "revenue")
	got := w.list(owner, ptr(w.a1))
	if len(got) != 2 || got["linh@hotel.com"].Email == "" || got["lan@hotel.com"].Email == "" {
		t.Fatalf("%+v", got)
	}
}

func TestStaffRoutesNeedUsersManage(t *testing.T) {
	w := newWorld(t)
	target := w.member("lan@hotel.com", w.orgA, ptr(w.a1), "front_desk")
	for _, role := range []string{"front_desk", "revenue", "housekeeping"} {
		p := w.member(role+"@hotel.com", w.orgA, ptr(w.a1), role)
		id := target.id.String()
		w.do(p, "GET", "/api/staff", ptr(w.a1), "").want(t, 403)
		w.do(p, "PATCH", "/api/staff/"+id, ptr(w.a1), `{"role":"manager"}`).want(t, 403)
		w.do(p, "POST", "/api/staff/"+id+"/deactivate", ptr(w.a1), "").want(t, 403)
		w.do(p, "POST", "/api/staff/"+id+"/reactivate", ptr(w.a1), "").want(t, 403)
		w.do(p, "POST", "/api/staff/"+id+"/reset-sign-in", ptr(w.a1), "").want(t, 403)
	}
	w.do(nil, "GET", "/api/staff", ptr(w.a1), "").want(t, 401)
}

func TestRoleChangeAppliesOnTheNextRequest(t *testing.T) {
	w := newWorld(t)
	owner := w.member("linh@hotel.com", w.orgA, nil, "owner")
	target := w.member("lan@hotel.com", w.orgA, ptr(w.a1), "front_desk")
	w.do(target, "GET", "/api/me", nil, "").want(t, 200, `"role":"front_desk"`)
	w.do(owner, "PATCH", "/api/staff/"+target.id.String(), ptr(w.a1), `{"role":"revenue"}`).want(t, 204)
	w.do(target, "GET", "/api/me", nil, "").want(t, 200, `"role":"revenue"`)
	for _, bad := range []string{`{"role":"janitor"}`, `{"role":"owner"}`, `{}`, `{not json`} {
		if r := w.do(owner, "PATCH", "/api/staff/"+target.id.String(), ptr(w.a1), bad); r.status != 400 {
			t.Errorf("%s: %d, want 400", bad, r.status)
		}
	}
}

func TestAManagerCannotTouchAnOwner(t *testing.T) {
	w := newWorld(t)
	owner := w.member("linh@hotel.com", w.orgA, nil, "owner")
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	id := owner.id.String()
	w.do(mgr, "PATCH", "/api/staff/"+id, ptr(w.a1), `{"role":"revenue"}`).want(t, 404)
	w.do(mgr, "POST", "/api/staff/"+id+"/deactivate", ptr(w.a1), "").want(t, 404)
	w.do(mgr, "POST", "/api/staff/"+id+"/reset-sign-in", ptr(w.a1), "").want(t, 404)
}

func TestOnlyAnOwnerCanMoveSomeoneToAnotherProperty(t *testing.T) {
	w := newWorld(t)
	owner := w.member("linh@hotel.com", w.orgA, nil, "owner")
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	target := w.member("lan@hotel.com", w.orgA, ptr(w.a1), "front_desk")
	id := target.id.String()
	move := func(to uuid.UUID) string { return fmt.Sprintf(`{"propertyId":%q}`, to) }
	w.do(mgr, "PATCH", "/api/staff/"+id, ptr(w.a1), move(w.a2)).want(t, 403, "forbidden")
	w.do(owner, "PATCH", "/api/staff/"+id, ptr(w.a1), move(w.b1)).want(t, 404)
	w.do(owner, "PATCH", "/api/staff/"+id, ptr(w.a1), move(w.a2)).want(t, 204)
	if got := w.list(owner, ptr(w.a2))["lan@hotel.com"]; got.PropertyName != "a2" {
		t.Fatalf("%+v", got)
	}
	if got := w.list(owner, ptr(w.a1))["lan@hotel.com"]; got.Email != "" {
		t.Fatal("the person is still listed at the old property")
	}
	// Moving onto a property where the person already works is refused.
	w.join(target, w.orgA, ptr(w.a1), "revenue")
	w.do(owner, "PATCH", "/api/staff/"+id, ptr(w.a1), move(w.a2)).want(t, 409, "already_member")
}

func TestTheLastOwnerCannotBeDeactivated(t *testing.T) {
	w := newWorld(t)
	first := w.member("linh@hotel.com", w.orgA, nil, "owner")
	w.do(first, "POST", "/api/staff/"+first.id.String()+"/deactivate", nil, "").want(t, 409, "last_owner")
	second := w.member("nam@hotel.com", w.orgA, nil, "owner")
	w.do(first, "POST", "/api/staff/"+second.id.String()+"/deactivate", nil, "").want(t, 204)
	w.do(first, "POST", "/api/staff/"+first.id.String()+"/deactivate", nil, "").want(t, 409, "last_owner")
	if got := w.memberships("linh@hotel.com"); len(got) != 1 || got[0].Status != "active" {
		t.Fatalf("%+v", got)
	}
}

func TestDeactivatingEndsThatOrganizationsSessionsOnly(t *testing.T) {
	w := newWorld(t)
	owner := w.member("linh@hotel.com", w.orgA, nil, "owner")
	x := w.member("lan@hotel.com", w.orgA, ptr(w.a1), "front_desk")
	w.join(x, w.orgB, ptr(w.b1), "front_desk") // the same person also works for organization B
	inB := w.asSession(w.newSession(x, w.orgB))
	w.do(x, "GET", "/api/me", nil, "").want(t, 200, `"name":"A"`)

	w.do(owner, "POST", "/api/staff/"+x.id.String()+"/deactivate", ptr(w.a1), "").want(t, 204)

	w.do(x, "GET", "/api/me", nil, "").want(t, 401)
	w.do(inB, "GET", "/api/me", nil, "").want(t, 200, `"name":"B"`)
	fresh := w.asSession(w.newSession(x, w.orgA))
	w.do(fresh, "GET", "/api/me", nil, "").want(t, 403, "no_access")
	if got := w.list(owner, ptr(w.a1))["lan@hotel.com"]; got.Status != "deactivated" {
		t.Fatalf("%+v", got)
	}
	// Organization B never lost them.
	for _, m := range w.memberships("lan@hotel.com") {
		if m.Property != nil && *m.Property == w.b1 && m.Status != "active" {
			t.Fatal("deactivating in A deactivated the person in B")
		}
	}
}

func TestReactivatingRestoresAccess(t *testing.T) {
	w := newWorld(t)
	owner := w.member("linh@hotel.com", w.orgA, nil, "owner")
	x := w.member("lan@hotel.com", w.orgA, ptr(w.a1), "front_desk")
	w.do(owner, "POST", "/api/staff/"+x.id.String()+"/deactivate", ptr(w.a1), "").want(t, 204)
	w.do(owner, "POST", "/api/staff/"+x.id.String()+"/reactivate", ptr(w.a1), "").want(t, 204)
	fresh := w.asSession(w.newSession(x, w.orgA))
	w.do(fresh, "GET", "/api/me", nil, "").want(t, 200)
}

func TestResetSignInEndsSessionsAndForgetsDevices(t *testing.T) {
	w := newWorld(t)
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	x := w.member("lan@hotel.com", w.orgA, ptr(w.a1), "front_desk")
	second := w.asSession(w.newSession(x, w.orgA))
	var device string
	must(t, db.WithoutOrg(ctx, w.pool, func(tx pgx.Tx) (e error) { device, e = session.TrustDevice(ctx, tx, x.id, w.clock()); return }))
	w.do(mgr, "POST", "/api/staff/"+x.id.String()+"/reset-sign-in", ptr(w.a1), "").want(t, 204)
	w.do(x, "GET", "/api/me", nil, "").want(t, 401)
	w.do(second, "GET", "/api/me", nil, "").want(t, 401)
	trusted := true
	must(t, db.WithoutOrg(ctx, w.pool, func(tx pgx.Tx) error { trusted = session.IsTrusted(ctx, tx, x.id, device, w.clock()); return nil }))
	if trusted {
		t.Fatal("the trusted device survived a sign-in reset")
	}
	w.do(mgr, "GET", "/api/me", nil, "").want(t, 200) // the manager's own session is untouched
}

func TestActionsOnPeopleOutsideYourScopeAre404(t *testing.T) {
	w := newWorld(t)
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	owner := w.member("linh@hotel.com", w.orgA, nil, "owner")
	atA2 := w.member("minh@hotel.com", w.orgA, ptr(w.a2), "revenue")
	inB := w.member("outsider@hotel.com", w.orgB, ptr(w.b1), "front_desk")
	for name, c := range map[string]struct {
		as     *person
		header *uuid.UUID
		target string
	}{
		"manager of a1 on someone at a2":     {mgr, ptr(w.a1), atA2.id.String()},
		"owner of A on someone in B":         {owner, ptr(w.a1), inB.id.String()},
		"a user id that does not exist":      {owner, ptr(w.a1), uuid.NewString()},
		"an id that is not a uuid":           {owner, ptr(w.a1), "not-a-uuid"},
		"owner without header on a1's staff": {owner, nil, atA2.id.String()},
	} {
		for _, path := range []string{"/deactivate", "/reactivate", "/reset-sign-in"} {
			want := 404
			if name == "owner without header on a1's staff" {
				want = 404 // without a header an owner addresses organization-wide memberships, and this person has none
			}
			if r := w.do(c.as, "POST", "/api/staff/"+c.target+path, c.header, ""); r.status != want {
				t.Errorf("%s %s: %d %s", name, path, r.status, r.body)
			}
		}
		if r := w.do(c.as, "PATCH", "/api/staff/"+c.target, c.header, `{"role":"manager"}`); r.status != 404 {
			t.Errorf("%s PATCH: %d", name, r.status)
		}
	}
}

func TestAnInviteRowIsNotAPerson(t *testing.T) {
	w := newWorld(t)
	owner := w.member("linh@hotel.com", w.orgA, nil, "owner")
	w.invite(owner, ptr(w.a1), "new@hotel.com", "front_desk").want(t, 201)
	inviteID := w.list(owner, nil)["new@hotel.com"].ID
	w.do(owner, "POST", "/api/staff/"+inviteID+"/deactivate", ptr(w.a1), "").want(t, 404)
}
