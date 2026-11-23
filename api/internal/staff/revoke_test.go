package staff_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func (w *world) inviteID(p *person, property *uuid.UUID, email, role string) string {
	w.t.Helper()
	var out struct{ ID string }
	jsonOf(w.t, w.invite(p, property, email, role).want(w.t, 201), &out)
	return out.ID
}

func TestAnInviteCanBeRevoked(t *testing.T) {
	w := newWorld(t)
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	id := w.inviteID(mgr, ptr(w.a1), "typo@hotel.com", "manager")
	token := w.lastLinkToken()
	w.do(mgr, "DELETE", "/api/staff-invites/"+id, ptr(w.a1), "").want(t, 204)
	if _, has := w.list(mgr, ptr(w.a1))["typo@hotel.com"]; has {
		t.Fatal("a revoked invite is still listed")
	}
	w.do(nil, "GET", "/api/invites/"+token, nil, "").want(t, 404)
	w.do(nil, "POST", "/api/invites/"+token+"/accept", nil, `{"name":"X","password":"`+goodPassword+`"}`).want(t, 404)
	if e := w.only(w.orgA, "user.invite_revoked"); !strings.Contains(e.Label, "typo@hotel.com") {
		t.Fatalf("%+v", e)
	}
}

func TestOnlyInvitesInYourScopeCanBeRevoked(t *testing.T) {
	w := newWorld(t)
	owner := w.member("linh@hotel.com", w.orgA, nil, "owner")
	mgr1 := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	mgr2 := w.member("lan@hotel.com", w.orgA, ptr(w.a2), "manager")
	desk := w.member("minh@hotel.com", w.orgA, ptr(w.a1), "front_desk")
	other := w.inviteID(mgr2, ptr(w.a2), "a2@hotel.com", "revenue")
	ownerInvite := w.inviteID(owner, nil, "boss@hotel.com", "owner")
	mine := w.inviteID(mgr1, ptr(w.a1), "a1@hotel.com", "revenue")

	w.do(mgr1, "DELETE", "/api/staff-invites/"+other, ptr(w.a1), "").want(t, 404)
	w.do(mgr1, "DELETE", "/api/staff-invites/"+ownerInvite, ptr(w.a1), "").want(t, 404)
	w.do(mgr1, "DELETE", "/api/staff-invites/"+uuid.New().String(), ptr(w.a1), "").want(t, 404)
	w.do(mgr1, "DELETE", "/api/staff-invites/not-an-id", ptr(w.a1), "").want(t, 404)
	w.do(desk, "DELETE", "/api/staff-invites/"+mine, ptr(w.a1), "").want(t, 403)
	w.do(nil, "DELETE", "/api/staff-invites/"+mine, nil, "").want(t, 401)
	w.do(owner, "DELETE", "/api/staff-invites/"+ownerInvite, nil, "").want(t, 204)
}

func TestPreviewSaysWhetherThePersonAlreadyHasAnAccount(t *testing.T) {
	w := newWorld(t)
	ownerB := w.member("boss@hotel.com", w.orgB, nil, "owner")
	w.member("lan@hotel.com", w.orgA, ptr(w.a1), "front_desk")
	w.invite(ownerB, ptr(w.b1), "lan@hotel.com", "manager").want(t, 201)
	w.do(nil, "GET", "/api/invites/"+w.lastLinkToken(), nil, "").want(t, 200, `"hasAccount":true`)
	w.invite(ownerB, ptr(w.b1), "new@hotel.com", "manager").want(t, 201)
	w.do(nil, "GET", "/api/invites/"+w.lastLinkToken(), nil, "").want(t, 200, `"hasAccount":false`)
}
