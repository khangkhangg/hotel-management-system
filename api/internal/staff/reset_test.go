package staff_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/account"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/password"
	"hotelpms/api/internal/session"
)

func (w *world) requestReset(email string) reply {
	return w.do(nil, "POST", "/api/password-reset", nil, fmt.Sprintf(`{"email":%q}`, email))
}

func (w *world) useReset(token, pw string) reply {
	return w.do(nil, "POST", "/api/password-reset/"+token, nil, fmt.Sprintf(`{"password":%q}`, pw))
}

func TestResetAnswersTheSameForAnUnknownEmail(t *testing.T) {
	w := newWorld(t)
	w.member("lan@hotel.com", w.orgA, ptr(w.a1), "front_desk")
	known := w.requestReset(" Lan@Hotel.com ").want(t, 204)
	if len(w.mail.Sent()) != 1 {
		t.Fatalf("%d emails for a known address", len(w.mail.Sent()))
	}
	unknown := w.requestReset("nobody@hotel.com").want(t, 204)
	if known.body != unknown.body || len(w.mail.Sent()) != 1 {
		t.Fatal("an unknown address was treated differently from a known one")
	}
	msg := w.mail.Sent()[0]
	if msg.To != "lan@hotel.com" || !strings.Contains(msg.Text, "30 minutes") || !strings.Contains(msg.Text, "https://app.example.com/reset/") {
		t.Fatalf("%+v", msg)
	}
}

func TestResetLinkIsValidFor30MinutesAndOnce(t *testing.T) {
	w := newWorld(t)
	w.member("lan@hotel.com", w.orgA, ptr(w.a1), "front_desk")
	w.requestReset("lan@hotel.com").want(t, 204)
	token := w.lastLinkToken()
	w.advance(29 * time.Minute)
	w.useReset(token, "a brand new password").want(t, 204)
	w.useReset(token, "another new password").want(t, 400, "link_invalid", "This link has expired. Ask for a new one.")

	w.requestReset("lan@hotel.com").want(t, 204)
	late := w.lastLinkToken()
	w.advance(31 * time.Minute)
	w.useReset(late, "a brand new password").want(t, 400, "link_invalid")
}

func TestResetChangesThePasswordAndEndsEverything(t *testing.T) {
	w := newWorld(t)
	x := w.member("lan@hotel.com", w.orgA, ptr(w.a1), "front_desk")
	other := w.asSession(w.newSession(x, w.orgA))
	var device string
	must(t, db.WithoutOrg(ctx, w.pool, func(tx pgx.Tx) (e error) { device, e = session.TrustDevice(ctx, tx, x.id, w.clock()); return }))
	w.do(x, "GET", "/api/me", nil, "").want(t, 200)

	w.requestReset("lan@hotel.com").want(t, 204)
	w.useReset(w.lastLinkToken(), "a brand new password").want(t, 204)

	w.do(x, "GET", "/api/me", nil, "").want(t, 401)
	w.do(other, "GET", "/api/me", nil, "").want(t, 401)
	var user account.User
	trusted := true
	must(t, db.WithoutOrg(ctx, w.pool, func(tx pgx.Tx) (e error) {
		user, e = account.ByID(ctx, tx, x.id)
		trusted = session.IsTrusted(ctx, tx, x.id, device, w.clock())
		return
	}))
	if !password.Verify(user.PasswordHash, "a brand new password") || password.Verify(user.PasswordHash, goodPassword) {
		t.Fatal("the password was not replaced")
	}
	if trusted {
		t.Fatal("a trusted device survived a password reset")
	}
}

func TestThreeResetEmailsPerHour(t *testing.T) {
	w := newWorld(t)
	w.member("lan@hotel.com", w.orgA, ptr(w.a1), "front_desk")
	for i := 0; i < 4; i++ {
		w.requestReset("lan@hotel.com").want(t, 204) // the fourth still answers 204 so nothing is revealed
	}
	if len(w.mail.Sent()) != 3 {
		t.Fatalf("%d emails, want 3", len(w.mail.Sent()))
	}
	w.advance(61 * time.Minute)
	w.requestReset("lan@hotel.com").want(t, 204)
	if len(w.mail.Sent()) != 4 {
		t.Fatalf("%d emails after an hour, want 4", len(w.mail.Sent()))
	}
}

func TestResetRejectsWeakAndBreachedPasswordsButKeepsTheLink(t *testing.T) {
	w := newWorld(t)
	w.member("lan@hotel.com", w.orgA, ptr(w.a1), "front_desk")
	w.requestReset("lan@hotel.com").want(t, 204)
	token := w.lastLinkToken()
	w.useReset(token, "short").want(t, 400, "password_too_short", "Use at least 12 characters.")
	w.useReset(token, "correct horse battery").want(t, 400, "password_breached")
	w.useReset(token, "a brand new password").want(t, 204)
}

func TestResetWithABadTokenOrBodyIs400(t *testing.T) {
	w := newWorld(t)
	w.useReset("made-up", "a brand new password").want(t, 400, "link_invalid")
	w.do(nil, "POST", "/api/password-reset/made-up", nil, `{oops`).want(t, 400)
	w.do(nil, "POST", "/api/password-reset", nil, `{oops`).want(t, 400)
	w.requestReset("").want(t, 204) // an empty address is just an unknown one
}

func TestResetForADeactivatedAccountSendsNothing(t *testing.T) {
	w := newWorld(t)
	x := w.member("lan@hotel.com", w.orgA, ptr(w.a1), "front_desk")
	_, err := w.pool.Exec(ctx, `update app_user set status = 'deactivated' where id = $1`, x.id)
	must(t, err)
	w.requestReset("lan@hotel.com").want(t, 204)
	if len(w.mail.Sent()) != 0 {
		t.Fatal("a deactivated account was sent a reset link")
	}
}

func TestAFailedResetEmailStillAnswers204AndDoesNotCount(t *testing.T) {
	w := newWorld(t)
	w.member("lan@hotel.com", w.orgA, ptr(w.a1), "front_desk")
	w.mail.FailNext()
	w.requestReset("lan@hotel.com").want(t, 204)
	if len(w.mail.Sent()) != 0 {
		t.Fatal("the failed email was recorded as sent")
	}
	for i := 0; i < 3; i++ {
		w.requestReset("lan@hotel.com").want(t, 204)
	}
	if len(w.mail.Sent()) != 3 {
		t.Fatalf("%d emails, want 3: the failed attempt must not use up the hourly limit", len(w.mail.Sent()))
	}
}
