package signin_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/account"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/db/testdb"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/mail"
	"hotelpms/api/internal/password"
	"hotelpms/api/internal/signin"
	"hotelpms/api/internal/tenant"
)

var ctx = context.Background()

const (
	goodPassword = "twelve chars ok"
	chromeOnWin  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"
)

// ---------- the app under test ----------

type app struct {
	t    *testing.T
	pool *pgxpool.Pool
	mail *mail.Fake
	srv  *httptest.Server
	mu   sync.Mutex
	now  time.Time
	main *site
}

func newApp(t *testing.T, extra ...httpx.Mount) *app {
	t.Helper()
	a := &app{t: t, pool: testdb.New(t), mail: mail.NewFake(), now: time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)}
	a.srv = httptest.NewServer(httpx.NewRouter(httpx.Deps{Pool: a.pool, Mail: a.mail, Now: a.clock}, append([]httpx.Mount{signin.Mount}, extra...)...))
	t.Cleanup(a.srv.Close)
	return a
}

func (a *app) clock() time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.now
}

func (a *app) advance(d time.Duration) {
	a.mu.Lock()
	a.now = a.now.Add(d)
	a.mu.Unlock()
}

type site struct {
	org, property uuid.UUID
	name          string
}

func (a *app) newSite(name string) *site {
	a.t.Helper()
	s := &site{name: name}
	must(a.t, db.WithoutOrg(ctx, a.pool, func(tx pgx.Tx) (e error) { s.org, e = tenant.CreateOrganization(ctx, tx, name); return }))
	must(a.t, db.WithOrg(ctx, a.pool, s.org, func(tx pgx.Tx) (e error) {
		s.property, e = tenant.CreateProperty(ctx, tx, s.org, tenant.PropertyInput{Name: "Main hotel", Slug: strings.ToLower(strings.ReplaceAll(name, " ", "-")), TimeZone: "UTC", Currency: "USD", DefaultLocale: "en"})
		return
	}))
	return s
}

func (a *app) site() *site {
	if a.main == nil {
		a.main = a.newSite("Sample Hotel")
	}
	return a.main
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// seedUser creates a person with a password. role "" means no membership. An owner works organization-wide.
func (a *app) seedUser(email, pw string, s *site, role string) uuid.UUID {
	a.t.Helper()
	hash, err := password.Hash(pw)
	must(a.t, err)
	var id uuid.UUID
	must(a.t, db.WithoutOrg(ctx, a.pool, func(tx pgx.Tx) error {
		var e error
		if id, e = account.Create(ctx, tx, email, "Mai Pham"); e != nil {
			return e
		}
		return account.SetPassword(ctx, tx, id, hash)
	}))
	if role != "" {
		a.addMembership(id, s, role)
	}
	return id
}

func (a *app) addMembership(user uuid.UUID, s *site, role string) {
	a.t.Helper()
	var prop *uuid.UUID
	if role != "owner" {
		prop = &s.property
	}
	must(a.t, db.WithOrg(ctx, a.pool, s.org, func(tx pgx.Tx) error { return tenant.AddMembership(ctx, tx, s.org, user, prop, role) }))
}

var sixDigits = regexp.MustCompile(`\b(\d{6})\b`)

func (a *app) lastCode() string {
	a.t.Helper()
	sent := a.mail.Sent()
	if len(sent) == 0 {
		a.t.Fatal("no email was sent")
	}
	m := sixDigits.FindStringSubmatch(sent[len(sent)-1].Text)
	if m == nil {
		a.t.Fatalf("no code in %q", sent[len(sent)-1].Text)
	}
	return m[1]
}

// ---------- a browser ----------

type client struct {
	a      *app
	hc     *http.Client
	csrf   string
	noCSRF bool
}

func (a *app) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{a: a, hc: &http.Client{Jar: jar}}
}

type result struct {
	t      *testing.T
	status int
	body   string
	took   time.Duration
}

func (r result) want(status int, contains ...string) result {
	r.t.Helper()
	if r.status != status {
		r.t.Fatalf("status %d, want %d; body %s", r.status, status, r.body)
	}
	for _, c := range contains {
		if !strings.Contains(r.body, c) {
			r.t.Fatalf("body %s does not contain %q", r.body, c)
		}
	}
	return r
}

func (c *client) send(method, path, body string) result {
	t := c.a.t
	t.Helper()
	if method != "GET" && !c.noCSRF && c.csrf == "" {
		resp, err := c.hc.Get(c.a.srv.URL + "/api/csrf")
		must(t, err)
		var j struct{ Token string }
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		must(t, json.Unmarshal(raw, &j))
		c.csrf = j.Token
	}
	req, err := http.NewRequest(method, c.a.srv.URL+path, strings.NewReader(body))
	must(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", chromeOnWin)
	if method != "GET" && !c.noCSRF {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	start := time.Now()
	resp, err := c.hc.Do(req)
	must(t, err)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return result{t: t, status: resp.StatusCode, body: string(raw), took: time.Since(start)}
}

func (c *client) get(path string) result        { return c.send("GET", path, "") }
func (c *client) post(path, body string) result { return c.send("POST", path, body) }
func (c *client) cookie(name string) string {
	u, _ := url.Parse(c.a.srv.URL)
	for _, ck := range c.hc.Jar.Cookies(u) {
		if ck.Name == name {
			return ck.Value
		}
	}
	return ""
}

func creds(email, pw string) string { return fmt.Sprintf(`{"email":%q,"password":%q}`, email, pw) }

func (c *client) signIn(email, pw string) result { return c.post("/api/sign-in", creds(email, pw)) }

func (c *client) enterCode(code string, trust bool) result {
	return c.post("/api/sign-in/code", fmt.Sprintf(`{"code":%q,"trustDevice":%v}`, code, trust))
}

// fullSignIn runs both steps and returns the result of the code step.
func (c *client) fullSignIn(email, pw string, trust bool) result {
	c.a.t.Helper()
	c.signIn(email, pw).want(200, `"next":"code"`)
	return c.enterCode(c.a.lastCode(), trust)
}

// ---------- the tests ----------

func TestPasswordAloneGivesNoAccess(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	c := a.client()
	c.signIn("mai@hotel.com", goodPassword).want(200, `"next":"code"`)
	c.get("/api/me").want(401, "unauthenticated")
}

func TestFullSignInAndMe(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	c := a.client()
	c.signIn(" Mai@Hotel.com", goodPassword).want(200, `"next":"code"`)
	pending := c.cookie("sid")
	c.enterCode(a.lastCode(), false).want(200, `"next":"done"`)
	if c.cookie("sid") == "" || c.cookie("sid") == pending {
		t.Fatal("the session id was not replaced after the second step")
	}
	var me struct {
		User         struct{ ID, Name, Email string }
		Organization struct{ ID, Name string }
		Memberships  []struct{ PropertyID, PropertyName, Role string }
		ViewAs       string
	}
	r := c.get("/api/me").want(200)
	must(t, json.Unmarshal([]byte(r.body), &me))
	if me.User.Email != "mai@hotel.com" || me.User.Name != "Mai Pham" || me.Organization.Name != "Sample Hotel" || me.ViewAs != "" ||
		len(me.Memberships) != 1 || me.Memberships[0].Role != "front_desk" || me.Memberships[0].PropertyName != "Main hotel" {
		t.Fatalf("%+v", me)
	}
}

func TestEmailNamesTheBrowser(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	a.client().signIn("mai@hotel.com", goodPassword).want(200)
	if text := a.mail.Sent()[0].Text; !strings.Contains(text, "Chrome on Windows") {
		t.Fatalf("the email does not name the browser:\n%s", text)
	}
}

func TestUnknownAndWrongLookTheSame(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	var wrong, unknown time.Duration
	var wrongBody, unknownBody string
	for i := 0; i < 3; i++ {
		w := a.client().signIn("mai@hotel.com", "wrong wrong wrong").want(401, "invalid_credentials")
		u := a.client().signIn(fmt.Sprintf("nobody%d@hotel.com", i), "wrong wrong wrong").want(401, "invalid_credentials")
		wrong, unknown, wrongBody, unknownBody = wrong+w.took, unknown+u.took, w.body, u.body
	}
	if wrongBody != unknownBody {
		t.Fatalf("different bodies:\n%s\n%s", wrongBody, unknownBody)
	}
	if !strings.Contains(wrongBody, "The email or password is not right.") {
		t.Fatalf("copy: %s", wrongBody)
	}
	if unknown*2 < wrong || wrong-unknown > 450*time.Millisecond || unknown-wrong > 450*time.Millisecond {
		t.Fatalf("timing reveals whether the email exists: wrong %v, unknown %v over 3 tries", wrong, unknown)
	}
	if len(a.mail.Sent()) != 0 {
		t.Fatal("a failed sign-in sent an email")
	}
}

func TestDeactivatedUserCannotSignIn(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	_, err := a.pool.Exec(ctx, `update app_user set status = 'deactivated'`)
	must(t, err)
	a.client().signIn("mai@hotel.com", goodPassword).want(401, "invalid_credentials")
}

func TestUserWithoutAPasswordCannotSignInWithOne(t *testing.T) {
	a := newApp(t)
	must(t, db.WithoutOrg(ctx, a.pool, func(tx pgx.Tx) error { _, e := account.Create(ctx, tx, "google@hotel.com", "G"); return e }))
	a.client().signIn("google@hotel.com", goodPassword).want(401, "invalid_credentials")
	a.client().signIn("google@hotel.com", "").want(401, "invalid_credentials")
}

func TestTrustedDeviceSkipsTheCode(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	c := a.client()
	c.fullSignIn("mai@hotel.com", goodPassword, true).want(200, `"next":"done"`)
	c.post("/api/sign-out", "").want(204)
	c.get("/api/me").want(401)
	sent := len(a.mail.Sent())
	c.signIn("mai@hotel.com", goodPassword).want(200, `"next":"done"`)
	c.get("/api/me").want(200)
	if len(a.mail.Sent()) != sent {
		t.Fatal("a trusted device was still emailed a code")
	}
	// Another browser has no trusted-device cookie, so it still needs the code.
	a.client().signIn("mai@hotel.com", goodPassword).want(200, `"next":"code"`)
}

func TestDeviceIsNotTrustedUnlessAsked(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	c := a.client()
	c.fullSignIn("mai@hotel.com", goodPassword, false).want(200)
	c.post("/api/sign-out", "").want(204)
	c.signIn("mai@hotel.com", goodPassword).want(200, `"next":"code"`)
}

func TestLockAfterTenFailures(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	c := a.client()
	for i := 0; i < 10; i++ {
		c.signIn("mai@hotel.com", "wrong wrong wrong").want(401, "invalid_credentials")
	}
	c.signIn("mai@hotel.com", goodPassword).want(429, "too_many_attempts", "Try again in 15 minutes")
	// The lock is keyed on the typed email, so an unknown address behaves the same and nothing is revealed.
	for i := 0; i < 10; i++ {
		c.signIn("ghost@hotel.com", "wrong wrong wrong").want(401)
	}
	c.signIn("ghost@hotel.com", goodPassword).want(429, "too_many_attempts")
	a.advance(16 * time.Minute)
	c.signIn("mai@hotel.com", goodPassword).want(200, `"next":"code"`)
}

func TestNineFailuresDoNotLock(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	c := a.client()
	for i := 0; i < 9; i++ {
		c.signIn("mai@hotel.com", "wrong wrong wrong").want(401)
	}
	c.signIn("mai@hotel.com", goodPassword).want(200, `"next":"code"`)
}

func TestLockAfterFiftyFailuresFromOneAddress(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	c := a.client()
	for i := 0; i < 50; i++ {
		c.signIn(fmt.Sprintf("guess%d@hotel.com", i), "wrong wrong wrong").want(401)
	}
	c.signIn("mai@hotel.com", goodPassword).want(429, "too_many_attempts")
}

func TestPostWithoutACSRFTokenIsRefused(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	c := a.client()
	c.noCSRF = true
	c.signIn("mai@hotel.com", goodPassword).want(403, "csrf")
	if len(a.mail.Sent()) != 0 {
		t.Fatal("a request without a token sent an email")
	}
}

func TestBadJSONIs400(t *testing.T) {
	a := newApp(t)
	a.client().post("/api/sign-in", `{not json`).want(400, "bad_request")
	a.client().post("/api/sign-in", ``).want(400, "bad_request")
}

func TestNoMembershipGets403(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "")
	c := a.client()
	c.fullSignIn("mai@hotel.com", goodPassword, false).want(200, `"next":"done"`)
	c.get("/api/me").want(403, "no_access", "Your account has no access. Ask your manager.")
}

func TestTwoOrganizationsMustChoose(t *testing.T) {
	a := newApp(t)
	first, second := a.newSite("First Hotel"), a.newSite("Second Hotel")
	id := a.seedUser("mai@hotel.com", goodPassword, first, "owner")
	a.addMembership(id, second, "owner")
	c := a.client()
	r := c.fullSignIn("mai@hotel.com", goodPassword, false).want(200, `"next":"choose_organization"`, "First Hotel", "Second Hotel")
	if !strings.Contains(r.body, second.org.String()) {
		t.Fatalf("the organization ids are missing: %s", r.body)
	}
	c.get("/api/me").want(401, "organization_required")
	c.post("/api/sign-in/organization", fmt.Sprintf(`{"organizationId":%q}`, second.org)).want(204)
	c.get("/api/me").want(200, "Second Hotel")
}

func TestOneOrganizationIsChosenAutomatically(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "manager")
	c := a.client()
	c.fullSignIn("mai@hotel.com", goodPassword, false).want(200, `"next":"done"`)
	c.get("/api/me").want(200, "Sample Hotel")
}

func TestCannotChooseAnOrganizationYouDoNotBelongTo(t *testing.T) {
	a := newApp(t)
	mine, other := a.newSite("Mine"), a.newSite("Not mine")
	id := a.seedUser("mai@hotel.com", goodPassword, mine, "owner")
	a.addMembership(id, a.newSite("Also mine"), "owner")
	c := a.client()
	c.fullSignIn("mai@hotel.com", goodPassword, false).want(200, "choose_organization")
	c.post("/api/sign-in/organization", fmt.Sprintf(`{"organizationId":%q}`, other.org)).want(403, "forbidden")
	c.post("/api/sign-in/organization", `{"organizationId":"not-a-uuid"}`).want(400, "bad_request")
	c.get("/api/me").want(401, "organization_required")
}

func TestWrongAndExpiredCodes(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	c := a.client()
	c.signIn("mai@hotel.com", goodPassword).want(200)
	good := a.lastCode()
	bad := "000000"
	if good == bad {
		bad = "111111"
	}
	c.enterCode(bad, false).want(400, "code_wrong", "That code is not right. Check the email and try again.")
	a.advance(11 * time.Minute)
	c.enterCode(good, false).want(400, "code_expired", "That code has expired. We can send a new one.")
}

func TestFiveWrongCodesCancelIt(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	c := a.client()
	c.signIn("mai@hotel.com", goodPassword).want(200)
	good := a.lastCode()
	bad := "000000"
	if good == bad {
		bad = "111111"
	}
	for i := 0; i < 5; i++ {
		c.enterCode(bad, false).want(400)
	}
	c.enterCode(good, false).want(400, "code_cancelled", "Too many wrong tries. We can send a new code.")
}

func TestResendMakesTheOldCodeUseless(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	c := a.client()
	c.signIn("mai@hotel.com", goodPassword).want(200)
	first := a.lastCode()
	c.post("/api/sign-in/code/resend", "").want(204)
	second := a.lastCode()
	if len(a.mail.Sent()) != 2 {
		t.Fatalf("%d emails, want 2", len(a.mail.Sent()))
	}
	if first != second {
		c.enterCode(first, false).want(400, "code_wrong")
	}
	c.enterCode(second, false).want(200, `"next":"done"`)
}

func TestSixthCodeInAnHourIsRefused(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	c := a.client()
	c.signIn("mai@hotel.com", goodPassword).want(200)
	for i := 0; i < 4; i++ {
		c.post("/api/sign-in/code/resend", "").want(204)
	}
	c.post("/api/sign-in/code/resend", "").want(429, "too_many_codes", "You asked for 5 codes this hour. Try again later.")
}

func TestEmailFailureIsReportedAndCanBeRetried(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	c := a.client()
	a.mail.FailNext()
	c.signIn("mai@hotel.com", goodPassword).want(503, "email_failed", "We could not send the code. Try again.")
	c.post("/api/sign-in/code/resend", "").want(204)
	c.enterCode(a.lastCode(), false).want(200, `"next":"done"`)
}

func TestCodeStepNeedsAPendingSession(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	a.client().enterCode("123456", false).want(401, "unauthenticated")
	c := a.client()
	c.fullSignIn("mai@hotel.com", goodPassword, false).want(200)
	c.enterCode("123456", false).want(401, "unauthenticated") // a full session is not pending
	c.post("/api/sign-in/code/resend", "").want(401)
}

func TestPendingSessionExpires(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	c := a.client()
	c.signIn("mai@hotel.com", goodPassword).want(200)
	a.advance(16 * time.Minute)
	c.enterCode(a.lastCode(), false).want(401, "unauthenticated")
}

func TestSignOutEndsTheSession(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	c := a.client()
	c.fullSignIn("mai@hotel.com", goodPassword, false).want(200)
	c.get("/api/me").want(200)
	c.post("/api/sign-out", "").want(204)
	c.get("/api/me").want(401)
	c.post("/api/sign-out", "").want(204) // signing out twice is harmless
}

func TestSignOutEverywhereEndsOtherSessionsAndForgetsDevices(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	laptop, phone := a.client(), a.client()
	laptop.fullSignIn("mai@hotel.com", goodPassword, true).want(200)
	phone.fullSignIn("mai@hotel.com", goodPassword, true).want(200)
	laptop.post("/api/sign-out/everywhere", "").want(204)
	phone.get("/api/me").want(401)
	laptop.get("/api/me").want(401)
	// Both devices were trusted; the reset means both need a code again.
	phone.signIn("mai@hotel.com", goodPassword).want(200, `"next":"code"`)
}

func TestSessionLastsUntilItIsIdle(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	c := a.client()
	c.fullSignIn("mai@hotel.com", goodPassword, false).want(200)
	a.advance(11 * time.Hour)
	c.get("/api/me").want(200)
	a.advance(13 * time.Hour)
	c.get("/api/me").want(401)
}

func TestRoutesRefuseTheWrongMethod(t *testing.T) {
	a := newApp(t)
	c := a.client()
	c.get("/api/sign-in").want(405)
	c.post("/api/me", "").want(405)
}

func TestDescribeBrowser(t *testing.T) {
	cases := map[string]string{
		chromeOnWin: "Chrome on Windows",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.1 Safari/605.1.15":                   "Safari on macOS",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.1 Mobile/15E148 Safari/604.1": "Safari on iPhone",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:120.0) Gecko/20100101 Firefox/120.0":                                                        "Firefox on Windows",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 Edg/130.0.0.0":           "Edge on Windows",
		"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36":                   "Chrome on Android",
		"":           "a browser we could not identify",
		"curl/8.4.0": "a browser we could not identify",
	}
	for ua, want := range cases {
		if got := signin.DescribeBrowser(ua); got != want {
			t.Errorf("%q: got %q, want %q", ua, got, want)
		}
	}
}

func TestDeactivatedMembershipGivesNoAccessAndHidesTheOrganization(t *testing.T) {
	a := newApp(t)
	first, second := a.newSite("First Hotel"), a.newSite("Second Hotel")
	id := a.seedUser("mai@hotel.com", goodPassword, first, "owner")
	a.addMembership(id, second, "owner")
	a.deactivateMemberships(first.org)
	c := a.client()
	// Only the second organization is left, so it is chosen automatically and the first never appears.
	r := c.fullSignIn("mai@hotel.com", goodPassword, false).want(200, `"next":"done"`)
	if strings.Contains(r.body, "First Hotel") {
		t.Fatalf("a deactivated organization is offered: %s", r.body)
	}
	c.get("/api/me").want(200, "Second Hotel")

	a.deactivateMemberships(second.org)
	c2 := a.client()
	c2.fullSignIn("mai@hotel.com", goodPassword, false).want(200, `"next":"done"`)
	c2.get("/api/me").want(403, "no_access")
}

// deactivateMemberships deactivates every membership inside each organization. Row-level security means
// the update only reaches rows when it runs with that organization's context.
func (a *app) deactivateMemberships(orgs ...uuid.UUID) {
	a.t.Helper()
	for _, org := range orgs {
		must(a.t, db.WithOrg(ctx, a.pool, org, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `update membership set status = 'deactivated'`)
			if err == nil && tag.RowsAffected() == 0 {
				err = fmt.Errorf("no membership was deactivated in %s", org)
			}
			return err
		}))
	}
}
