package signin_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/db"
	"hotelpms/api/internal/signin"
)

// fakeGoogle stands in for Google: it hands back whatever identity the test sets.
type fakeGoogle struct {
	mu        sync.Mutex
	identity  signin.GoogleIdentity
	err       error
	calls     int
	challenge string
	verifier  string
	code      string
}

func (f *fakeGoogle) AuthURL(state, challenge string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.challenge = challenge
	return "https://accounts.google.test/auth?state=" + url.QueryEscape(state) + "&code_challenge=" + url.QueryEscape(challenge)
}

func (f *fakeGoogle) Exchange(_ context.Context, code, verifier string) (signin.GoogleIdentity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.code, f.verifier = code, verifier
	return f.identity, f.err
}

func newGoogleApp(t *testing.T) (*app, *fakeGoogle) {
	g := &fakeGoogle{}
	return newApp(t, signin.MountGoogle(g)), g
}

// browser does not follow redirects, so a test can read where the server sent the person.
func (a *app) browser() *client {
	c := a.client()
	c.hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c
}

func (c *client) location(path string) (status int, loc string) {
	resp, err := c.hc.Get(c.a.srv.URL + path)
	must(c.a.t, err)
	resp.Body.Close()
	return resp.StatusCode, resp.Header.Get("Location")
}

// startGoogle runs /start and returns the state the server issued.
func (c *client) startGoogle() string {
	c.a.t.Helper()
	status, loc := c.location("/api/sign-in/google/start")
	if status != http.StatusFound || !strings.HasPrefix(loc, "https://accounts.google.test/auth") {
		c.a.t.Fatalf("start: %d %s", status, loc)
	}
	u, _ := url.Parse(loc)
	return u.Query().Get("state")
}

func (c *client) callback(state, code string) (int, string) {
	return c.location("/api/sign-in/google/callback?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state))
}

func TestGoogleStartSendsStateAndAPKCEChallenge(t *testing.T) {
	a, g := newGoogleApp(t)
	c := a.browser()
	state := c.startGoogle()
	if len(state) < 32 || c.cookie("gstate") != state {
		t.Fatalf("state %q is not also in the gstate cookie (%q)", state, c.cookie("gstate"))
	}
	g.identity = signin.GoogleIdentity{Subject: "g1", Email: "mai@hotel.com", EmailVerified: true}
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	c.callback(state, "auth-code")
	// PKCE: Google got sha256(verifier) at the start, and the server later proves it with the verifier.
	sum := sha256.Sum256([]byte(g.verifier))
	if want := base64.RawURLEncoding.EncodeToString(sum[:]); g.challenge != want || g.code != "auth-code" {
		t.Fatalf("challenge %q does not match sha256(verifier) %q", g.challenge, want)
	}
}

func TestGoogleUnknownEmailIsRefused(t *testing.T) {
	a, g := newGoogleApp(t)
	g.identity = signin.GoogleIdentity{Subject: "g1", Email: "stranger@gmail.com", EmailVerified: true}
	c := a.browser()
	status, loc := c.callback(c.startGoogle(), "code")
	if status != 302 || loc != "/sign-in?error=google_not_invited" {
		t.Fatalf("%d %s", status, loc)
	}
	var n int
	db.WithoutOrg(ctx, a.pool, func(tx pgx.Tx) error { return tx.QueryRow(ctx, `select count(*) from app_user`).Scan(&n) })
	if n != 0 || len(a.mail.Sent()) != 0 {
		t.Fatalf("a stranger left %d users and %d emails behind", n, len(a.mail.Sent()))
	}
}

func TestGoogleUnverifiedEmailIsRefused(t *testing.T) {
	a, g := newGoogleApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	g.identity = signin.GoogleIdentity{Subject: "g1", Email: "mai@hotel.com", EmailVerified: false}
	c := a.browser()
	if _, loc := c.callback(c.startGoogle(), "code"); loc != "/sign-in?error=google_not_invited" {
		t.Fatal(loc)
	}
}

func TestGoogleStillNeedsTheEmailCode(t *testing.T) {
	a, g := newGoogleApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	g.identity = signin.GoogleIdentity{Subject: "g1", Email: " MAI@Hotel.com ", EmailVerified: true}
	c := a.browser()
	if status, loc := c.callback(c.startGoogle(), "code"); status != 302 || loc != "/sign-in/code" {
		t.Fatalf("%d %s", status, loc)
	}
	c.get("/api/me").want(401, "unauthenticated")
	c.enterCode(a.lastCode(), false).want(200, `"next":"done"`)
	c.get("/api/me").want(200, "mai@hotel.com")
}

func TestGoogleOnlyPersonWithoutAPasswordCanSignIn(t *testing.T) {
	a, g := newGoogleApp(t)
	id := a.seedUser("lan@hotel.com", goodPassword, a.site(), "front_desk")
	_, err := a.pool.Exec(ctx, `update app_user set password_hash = null where id = $1`, id)
	must(t, err)
	g.identity = signin.GoogleIdentity{Subject: "g9", Email: "lan@hotel.com", EmailVerified: true}
	c := a.browser()
	if _, loc := c.callback(c.startGoogle(), "code"); loc != "/sign-in/code" {
		t.Fatal(loc)
	}
}

func TestGoogleSubjectIsBoundOnFirstUse(t *testing.T) {
	a, g := newGoogleApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	g.identity = signin.GoogleIdentity{Subject: "g1", Email: "mai@hotel.com", EmailVerified: true}
	c := a.browser()
	if _, loc := c.callback(c.startGoogle(), "code"); loc != "/sign-in/code" {
		t.Fatal(loc)
	}
	var bound string
	db.WithoutOrg(ctx, a.pool, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select coalesce(google_subject, '') from app_user`).Scan(&bound)
	})
	if bound != "g1" {
		t.Fatalf("the Google subject was not stored: %q", bound)
	}
	g.identity.Subject = "g2" // a different Google account that claims the same email address
	c2 := a.browser()
	if _, loc := c2.callback(c2.startGoogle(), "code"); loc != "/sign-in?error=google_not_invited" {
		t.Fatalf("a second Google account took over the email: %s", loc)
	}
	g.identity.Subject = "g1"
	c3 := a.browser()
	if _, loc := c3.callback(c3.startGoogle(), "code"); loc != "/sign-in/code" {
		t.Fatalf("the original account was refused: %s", loc)
	}
}

func TestGoogleStateMismatchIsRefusedWithoutCallingGoogle(t *testing.T) {
	a, g := newGoogleApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	g.identity = signin.GoogleIdentity{Subject: "g1", Email: "mai@hotel.com", EmailVerified: true}
	c := a.browser()
	c.startGoogle()
	if _, loc := c.callback("some-other-state", "code"); loc != "/sign-in?error=google_failed" {
		t.Fatal(loc)
	}
	other := a.browser() // a browser that never started: no cookie at all
	if _, loc := other.callback("anything", "code"); loc != "/sign-in?error=google_failed" {
		t.Fatal(loc)
	}
	if g.calls != 0 {
		t.Fatalf("Google was called %d times for a bad state", g.calls)
	}
}

func TestGoogleStateIsSingleUse(t *testing.T) {
	a, g := newGoogleApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	g.identity = signin.GoogleIdentity{Subject: "g1", Email: "mai@hotel.com", EmailVerified: true}
	c := a.browser()
	state := c.startGoogle()
	if _, loc := c.callback(state, "code"); loc != "/sign-in/code" {
		t.Fatal(loc)
	}
	if _, loc := c.callback(state, "code"); loc != "/sign-in?error=google_failed" {
		t.Fatalf("the same state worked twice: %s", loc)
	}
}

func TestGoogleStateExpiresAfterTenMinutes(t *testing.T) {
	a, g := newGoogleApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	g.identity = signin.GoogleIdentity{Subject: "g1", Email: "mai@hotel.com", EmailVerified: true}
	c := a.browser()
	state := c.startGoogle()
	a.advance(11 * time.Minute)
	if _, loc := c.callback(state, "code"); loc != "/sign-in?error=google_failed" {
		t.Fatal(loc)
	}
}

func TestGoogleFailuresRedirectInsteadOfShowingAnError(t *testing.T) {
	a, g := newGoogleApp(t)
	g.err = errors.New("google said no")
	c := a.browser()
	if status, loc := c.callback(c.startGoogle(), "code"); status != 302 || loc != "/sign-in?error=google_failed" {
		t.Fatalf("%d %s", status, loc)
	}
	if status, loc := c.location("/api/sign-in/google/callback"); status != 302 || loc != "/sign-in?error=google_failed" {
		t.Fatalf("missing parameters: %d %s", status, loc)
	}
	if status, loc := c.location("/api/sign-in/google/callback?error=access_denied&state=x"); status != 302 || loc != "/sign-in?error=google_failed" {
		t.Fatalf("the person pressed cancel at Google: %d %s", status, loc)
	}
}

func TestGoogleDeactivatedUserIsRefused(t *testing.T) {
	a, g := newGoogleApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	_, err := a.pool.Exec(ctx, `update app_user set status = 'deactivated'`)
	must(t, err)
	g.identity = signin.GoogleIdentity{Subject: "g1", Email: "mai@hotel.com", EmailVerified: true}
	c := a.browser()
	if _, loc := c.callback(c.startGoogle(), "code"); loc != "/sign-in?error=google_not_invited" {
		t.Fatal(loc)
	}
}

func TestGoogleTrustedDeviceSkipsTheCode(t *testing.T) {
	a, g := newGoogleApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	g.identity = signin.GoogleIdentity{Subject: "g1", Email: "mai@hotel.com", EmailVerified: true}
	c := a.browser()
	c.fullSignIn("mai@hotel.com", goodPassword, true).want(200)
	c.post("/api/sign-out", "").want(204)
	sent := len(a.mail.Sent())
	if status, loc := c.callback(c.startGoogle(), "code"); status != 302 || loc != "/" {
		t.Fatalf("%d %s", status, loc)
	}
	c.get("/api/me").want(200)
	if len(a.mail.Sent()) != sent {
		t.Fatal("a trusted device was emailed a code")
	}
}

func TestGoogleSendsPeopleWithSeveralOrganizationsToTheChooser(t *testing.T) {
	a, g := newGoogleApp(t)
	id := a.seedUser("mai@hotel.com", goodPassword, a.newSite("First Hotel"), "owner")
	a.addMembership(id, a.newSite("Second Hotel"), "owner")
	g.identity = signin.GoogleIdentity{Subject: "g1", Email: "mai@hotel.com", EmailVerified: true}
	c := a.browser()
	c.fullSignIn("mai@hotel.com", goodPassword, true).want(200, "choose_organization")
	c.post("/api/sign-out", "").want(204)
	if _, loc := c.callback(c.startGoogle(), "code"); loc != "/sign-in/organization" {
		t.Fatalf("got %s", loc)
	}
}

func TestGoogleRoutesRefuseTheWrongMethod(t *testing.T) {
	a, _ := newGoogleApp(t)
	a.client().post("/api/sign-in/google/start", "").want(405)
}

var _ = uuid.Nil

// The attack the state cookie stops: an attacker starts their own flow, then tricks the victim's browser
// into finishing it. The attacker's state is real and unused, so only the cookie can tell the two browsers apart.
func TestGoogleStateFromAnotherBrowserIsRefused(t *testing.T) {
	a, g := newGoogleApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	g.identity = signin.GoogleIdentity{Subject: "g1", Email: "mai@hotel.com", EmailVerified: true}
	attacker, victim := a.browser(), a.browser()
	attackerState := attacker.startGoogle()
	victim.startGoogle() // the victim has a gstate cookie, but a different one
	if _, loc := victim.callback(attackerState, "code"); loc != "/sign-in?error=google_failed" {
		t.Fatalf("a state from another browser was accepted: %s", loc)
	}
	if g.calls != 0 {
		t.Fatal("Google was called with a state that belongs to another browser")
	}
	// The attacker's own flow still works, so the refusal did not burn their state by accident.
	if _, loc := attacker.callback(attackerState, "code"); loc != "/sign-in/code" {
		t.Fatalf("the rightful browser was refused: %s", loc)
	}
}
