package staff_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/account"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/password"
)

func (w *world) invite(p *person, property *uuid.UUID, email, role string) reply {
	body := fmt.Sprintf(`{"email":%q,"role":%q`, email, role)
	if property != nil {
		body += fmt.Sprintf(`,"propertyId":%q`, property)
	}
	return w.do(p, "POST", "/api/staff/invites", property, body+"}")
}

func (w *world) accept(token, name, pw string) reply {
	return w.do(nil, "POST", "/api/invites/"+token+"/accept", nil, fmt.Sprintf(`{"name":%q,"password":%q}`, name, pw))
}

func TestManagerInvitesAndTheEmailCarriesALink(t *testing.T) {
	w := newWorld(t)
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	w.invite(mgr, ptr(w.a1), " New@Hotel.com ", "front_desk").want(t, 201, `"id"`)
	sent := w.mail.Sent()
	if len(sent) != 1 || sent[0].To != "new@hotel.com" {
		t.Fatalf("%+v", sent)
	}
	for _, want := range []string{"https://app.example.com/invite/", "7 days", "A", "front desk"} {
		if !strings.Contains(sent[0].Text, want) {
			t.Errorf("the invite email does not say %q:\n%s", want, sent[0].Text)
		}
	}
	w.do(nil, "GET", "/api/invites/"+w.lastLinkToken(), nil, "").
		want(t, 200, `"email":"new@hotel.com"`, `"organizationName":"A"`, `"propertyName":"a1"`, `"role":"front_desk"`)
}

func TestPreviewOfAnUnknownTokenIs404(t *testing.T) {
	w := newWorld(t)
	w.do(nil, "GET", "/api/invites/not-a-real-token", nil, "").want(t, 404, "not_found")
}

func TestInviteExpiresAfter7Days(t *testing.T) {
	w := newWorld(t)
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	w.invite(mgr, ptr(w.a1), "new@hotel.com", "front_desk").want(t, 201)
	token := w.lastLinkToken()
	w.advance(7*24*time.Hour - time.Minute)
	w.do(nil, "GET", "/api/invites/"+token, nil, "").want(t, 200)
	w.advance(2 * time.Minute)
	w.do(nil, "GET", "/api/invites/"+token, nil, "").want(t, 404)
	w.accept(token, "Lan Vo", goodPassword).want(t, 404)
}

func TestInviteWorksOnce(t *testing.T) {
	w := newWorld(t)
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	w.invite(mgr, ptr(w.a1), "new@hotel.com", "front_desk").want(t, 201)
	token := w.lastLinkToken()
	w.accept(token, "Lan Vo", goodPassword).want(t, 200)
	w.accept(token, "Lan Vo", goodPassword).want(t, 404)
	w.do(nil, "GET", "/api/invites/"+token, nil, "").want(t, 404)
}

func TestInviteAcceptedTwiceAtOnceMakesOneMembership(t *testing.T) {
	w := newWorld(t)
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	w.invite(mgr, ptr(w.a1), "new@hotel.com", "front_desk").want(t, 201)
	token := w.lastLinkToken()
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w.accept(token, "Lan Vo", goodPassword).status == 200 {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 1 || len(w.memberships("new@hotel.com")) != 1 {
		t.Fatalf("%d accepts succeeded and %d memberships exist, want 1 and 1", ok, len(w.memberships("new@hotel.com")))
	}
}

func TestOnlyAnOwnerCanInviteAnOwner(t *testing.T) {
	w := newWorld(t)
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	owner := w.member("linh@hotel.com", w.orgA, nil, "owner")
	w.invite(mgr, ptr(w.a1), "boss@hotel.com", "owner").want(t, 403, "forbidden")
	if len(w.mail.Sent()) != 0 {
		t.Fatal("a refused invite sent an email")
	}
	w.invite(owner, nil, "boss@hotel.com", "owner").want(t, 201)
	w.accept(w.lastLinkToken(), "The Boss", goodPassword).want(t, 200)
	got := w.memberships("boss@hotel.com")
	if len(got) != 1 || got[0].Role != "owner" || got[0].Property != nil {
		t.Fatalf("%+v: an invited owner must be organization-wide", got)
	}
}

func TestInviteNeedsUsersManage(t *testing.T) {
	w := newWorld(t)
	for _, role := range []string{"front_desk", "revenue", "housekeeping"} {
		p := w.member(role+"@hotel.com", w.orgA, ptr(w.a1), role)
		w.invite(p, ptr(w.a1), "new@hotel.com", "front_desk").want(t, 403)
	}
	w.invite(nil, ptr(w.a1), "new@hotel.com", "front_desk").want(t, 401)
	if len(w.mail.Sent()) != 0 {
		t.Fatal("a refused invite sent an email")
	}
}

func TestInviteTargetsThePropertyOfTheRequest(t *testing.T) {
	w := newWorld(t)
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	w.do(mgr, "POST", "/api/staff/invites", ptr(w.a1), fmt.Sprintf(`{"email":"x@hotel.com","role":"front_desk","propertyId":%q}`, w.a2)).want(t, 400)
	w.do(mgr, "POST", "/api/staff/invites", ptr(w.a2), fmt.Sprintf(`{"email":"x@hotel.com","role":"front_desk","propertyId":%q}`, w.a2)).want(t, 403)
	w.do(mgr, "POST", "/api/staff/invites", nil, `{"email":"x@hotel.com","role":"front_desk"}`).want(t, 403)
	w.do(mgr, "POST", "/api/staff/invites", ptr(w.b1), `{"email":"x@hotel.com","role":"front_desk"}`).want(t, 403)
	if len(w.mail.Sent()) != 0 {
		t.Fatal("a refused invite sent an email")
	}
}

func TestInviteValidation(t *testing.T) {
	w := newWorld(t)
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	for name, body := range map[string]string{
		"no at sign":   `{"email":"nope","role":"front_desk"}`,
		"empty email":  `{"email":"","role":"front_desk"}`,
		"unknown role": `{"email":"x@hotel.com","role":"janitor"}`,
		"no role":      `{"email":"x@hotel.com"}`,
		"too long":     `{"email":"` + strings.Repeat("a", 250) + `@hotel.com","role":"front_desk"}`,
		"not json":     `{oops`,
	} {
		if r := w.do(mgr, "POST", "/api/staff/invites", ptr(w.a1), body); r.status != 400 {
			t.Errorf("%s: status %d", name, r.status)
		}
	}
}

func TestInvitingAgainReplacesTheOldInvite(t *testing.T) {
	w := newWorld(t)
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	w.invite(mgr, ptr(w.a1), "new@hotel.com", "front_desk").want(t, 201)
	first := w.lastLinkToken()
	w.invite(mgr, ptr(w.a1), "new@hotel.com", "revenue").want(t, 201)
	second := w.lastLinkToken()
	w.do(nil, "GET", "/api/invites/"+first, nil, "").want(t, 404)
	w.do(nil, "GET", "/api/invites/"+second, nil, "").want(t, 200, `"role":"revenue"`)
}

func TestInvitingASomeoneWhoAlreadyWorksThereIsRefused(t *testing.T) {
	w := newWorld(t)
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	w.member("lan@hotel.com", w.orgA, ptr(w.a1), "front_desk")
	w.invite(mgr, ptr(w.a1), "Lan@hotel.com", "revenue").want(t, 409, "already_member")
	// A person who works at a different property can be invited to this one.
	w.member("minh@hotel.com", w.orgA, ptr(w.a2), "front_desk")
	w.invite(mgr, ptr(w.a1), "minh@hotel.com", "front_desk").want(t, 201)
}

func TestAnEmailFailureLeavesNoInviteBehind(t *testing.T) {
	w := newWorld(t)
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	w.mail.FailNext()
	w.invite(mgr, ptr(w.a1), "new@hotel.com", "front_desk").want(t, 503, "email_failed", "We could not send the invite. Try again.")
	w.invite(mgr, ptr(w.a1), "new@hotel.com", "front_desk").want(t, 201)
	if len(w.mail.Sent()) != 1 {
		t.Fatalf("%d emails", len(w.mail.Sent()))
	}
}

func TestAcceptCreatesTheAccountAndStartsTheCodeStep(t *testing.T) {
	w := newWorld(t)
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	w.invite(mgr, ptr(w.a1), "new@hotel.com", "front_desk").want(t, 201)
	r := w.accept(w.lastLinkToken(), "Lan Vo", goodPassword).want(t, 200, `"next":"code"`)
	if r.cookie("sid") == "" {
		t.Fatal("no session cookie after accepting")
	}
	if len(w.mail.Sent()) != 2 {
		t.Fatalf("%d emails, want the invite and then a code", len(w.mail.Sent()))
	}
	// The pending session cannot reach the app until the code is entered.
	pending := w.asSession(r.cookie("sid"))
	w.do(pending, "GET", "/api/me", nil, "").want(t, 401)
	code := regexp6.FindStringSubmatch(w.mail.Sent()[1].Text)[1]
	w.do(pending, "POST", "/api/sign-in/code", nil, fmt.Sprintf(`{"code":%q}`, code)).want(t, 200, `"next":"done"`)

	var user account.User
	must(t, db.WithoutOrg(ctx, w.pool, func(tx pgx.Tx) (e error) { user, e = account.ByEmail(ctx, tx, "new@hotel.com"); return }))
	if user.Name != "Lan Vo" || !password.Verify(user.PasswordHash, goodPassword) {
		t.Fatalf("%+v", user)
	}
	got := w.memberships("new@hotel.com")
	if len(got) != 1 || got[0].Role != "front_desk" || got[0].Property == nil || *got[0].Property != w.a1 || got[0].Status != "active" {
		t.Fatalf("%+v", got)
	}
}

func TestAcceptRejectsWeakAndBreachedPasswordsWithoutUsingTheInvite(t *testing.T) {
	w := newWorld(t)
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a1), "manager")
	w.invite(mgr, ptr(w.a1), "new@hotel.com", "front_desk").want(t, 201)
	token := w.lastLinkToken()
	w.accept(token, "Lan Vo", "short").want(t, 400, "password_too_short", "Use at least 12 characters.")
	w.accept(token, "Lan Vo", "correct horse battery").want(t, 400, "password_breached", "That password has appeared in a data breach. Choose another.")
	w.accept(token, "", goodPassword).want(t, 400, "bad_request")
	w.accept(token, "Lan Vo", goodPassword).want(t, 200)
}

func TestAcceptForAnExistingAccountAddsTheRoleAndKeepsThePassword(t *testing.T) {
	w := newWorld(t)
	mgr := w.member("daniel@hotel.com", w.orgA, ptr(w.a2), "manager")
	existing := w.member("lan@hotel.com", w.orgA, ptr(w.a1), "front_desk")
	var before account.User
	must(t, db.WithoutOrg(ctx, w.pool, func(tx pgx.Tx) (e error) { before, e = account.ByID(ctx, tx, existing.id); return }))
	w.invite(mgr, ptr(w.a2), "lan@hotel.com", "revenue").want(t, 201)
	mails := len(w.mail.Sent())
	r := w.accept(w.lastLinkToken(), "Someone Else", "a different password 99").want(t, 200, `"next":"sign_in"`)
	if r.cookie("sid") != "" || len(w.mail.Sent()) != mails {
		t.Fatal("accepting for an existing account must not start a session or send a code")
	}
	var after account.User
	must(t, db.WithoutOrg(ctx, w.pool, func(tx pgx.Tx) (e error) { after, e = account.ByID(ctx, tx, existing.id); return }))
	if after.PasswordHash != before.PasswordHash || after.Name != before.Name {
		t.Fatal("an invite link changed an existing account's password or name")
	}
	if got := w.memberships("lan@hotel.com"); len(got) != 2 {
		t.Fatalf("%+v", got)
	}
}

func TestAcceptNeedsABody(t *testing.T) {
	w := newWorld(t)
	w.do(nil, "POST", "/api/invites/whatever/accept", nil, "").want(t, 400)
}
