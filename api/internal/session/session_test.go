package session_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
	"hotelpms/api/internal/session"
	"hotelpms/api/internal/tenant"
)

var (
	ctx = context.Background()
	t0  = time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
)

func tx(t testing.TB, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	t.Helper()
	return db.WithoutOrg(ctx, pool, fn)
}

func seedUser(t *testing.T, pool *pgxpool.Pool, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := tx(t, pool, func(c pgx.Tx) (e error) { id, e = account.Create(ctx, c, email, "Mai"); return }); err != nil {
		t.Fatal(err)
	}
	return id
}

func create(t *testing.T, pool *pgxpool.Pool, user uuid.UUID, stage session.Stage, at time.Time) session.Session {
	t.Helper()
	var s session.Session
	if err := tx(t, pool, func(c pgx.Tx) (e error) { s, e = session.Create(ctx, c, user, stage, "Chrome on Windows", at); return }); err != nil {
		t.Fatal(err)
	}
	return s
}

func lookup(pool *pgxpool.Pool, id string, at time.Time) (s session.Session, err error) {
	err = db.WithoutOrg(ctx, pool, func(c pgx.Tx) (e error) { s, e = session.Lookup(ctx, c, id, at); return })
	return
}

func promote(pool *pgxpool.Pool, id string, at time.Time) (s session.Session, err error) {
	err = db.WithoutOrg(ctx, pool, func(c pgx.Tx) (e error) { s, e = session.Promote(ctx, c, id, at); return })
	return
}

func TestSessionExpiry(t *testing.T) {
	pool := testdb.New(t)
	u := seedUser(t, pool, "mai@hotel.com")
	s := create(t, pool, u, session.StageFull, t0)
	if _, err := lookup(pool, s.ID, t0.Add(11*time.Hour+59*time.Minute)); err != nil {
		t.Fatal("ended early:", err)
	}
	s2 := create(t, pool, u, session.StageFull, t0)
	if _, err := lookup(pool, s2.ID, t0.Add(12*time.Hour+time.Minute)); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("12 hour idle limit: %v", err)
	}
	s3 := create(t, pool, u, session.StageFull, t0)
	for h := 1; h <= 7*24; h += 6 {
		if _, err := lookup(pool, s3.ID, t0.Add(time.Duration(h)*time.Hour)); err != nil {
			t.Fatalf("active session ended at hour %d: %v", h, err)
		}
	}
	if _, err := lookup(pool, s3.ID, t0.Add(7*24*time.Hour+time.Minute)); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("7 day limit: %v", err)
	}
}

func TestLookupUnknownIsErrNotFound(t *testing.T) {
	pool := testdb.New(t)
	if _, err := lookup(pool, "nope", t0); !errors.Is(err, session.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestLookupRefreshesLastSeen(t *testing.T) {
	pool := testdb.New(t)
	s := create(t, pool, seedUser(t, pool, "mai@hotel.com"), session.StageFull, t0)
	at := t0.Add(5 * time.Hour)
	got, err := lookup(pool, s.ID, at)
	if err != nil || !got.LastSeenAt.Equal(at) || !got.CreatedAt.Equal(t0) {
		t.Fatalf("%+v %v", got, err)
	}
	// 5h + 11h = 16h after creation, but only 11h idle, so it is still valid.
	if _, err := lookup(pool, s.ID, at.Add(11*time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func TestExpiredSessionStaysExpiredAndPurgeDeletesIt(t *testing.T) {
	pool := testdb.New(t)
	u := seedUser(t, pool, "mai@hotel.com")
	old := create(t, pool, u, session.StageFull, t0)
	pending := create(t, pool, u, session.StagePending, t0)
	fresh := create(t, pool, u, session.StageFull, t0.Add(20*time.Hour))
	now := t0.Add(21 * time.Hour)
	for _, id := range []string{old.ID, pending.ID} {
		for i := 0; i < 2; i++ {
			if _, err := lookup(pool, id, now); !errors.Is(err, session.ErrExpired) {
				t.Fatalf("lookup %d of an expired session: %v", i, err)
			}
		}
	}
	var n int64
	if err := tx(t, pool, func(c pgx.Tx) (e error) { n, e = session.Purge(ctx, c, now); return }); err != nil || n != 2 {
		t.Fatalf("purged %d (%v), want the 2 expired sessions", n, err)
	}
	if _, err := lookup(pool, old.ID, now); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("a purged session is still known: %v", err)
	}
	if _, err := lookup(pool, fresh.ID, now); err != nil {
		t.Fatalf("Purge removed a live session: %v", err)
	}
}

func TestPendingSessionExpiresAfter15Minutes(t *testing.T) {
	pool := testdb.New(t)
	u := seedUser(t, pool, "mai@hotel.com")
	p := create(t, pool, u, session.StagePending, t0)
	if _, err := lookup(pool, p.ID, t0.Add(14*time.Minute)); err != nil {
		t.Fatal(err)
	}
	p2 := create(t, pool, u, session.StagePending, t0)
	if _, err := lookup(pool, p2.ID, t0.Add(16*time.Minute)); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("a pending session outlived the code window: %v", err)
	}
}

func TestPromoteIssuesNewID(t *testing.T) {
	pool := testdb.New(t)
	u := seedUser(t, pool, "mai@hotel.com")
	p := create(t, pool, u, session.StagePending, t0)
	f, err := promote(pool, p.ID, t0.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if f.ID == p.ID || f.Stage != session.StageFull || f.UserID != u {
		t.Fatalf("not rotated: %+v", f)
	}
	if _, err := lookup(pool, p.ID, t0.Add(time.Minute)); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("the old id still works: %v", err)
	}
	if got, err := lookup(pool, f.ID, t0.Add(time.Minute)); err != nil || got.Stage != session.StageFull {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestPromoteTwiceAtOnceSucceedsOnce(t *testing.T) {
	pool := testdb.New(t)
	p := create(t, pool, seedUser(t, pool, "mai@hotel.com"), session.StagePending, t0)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := promote(pool, p.ID, t0.Add(time.Minute)); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 1 {
		t.Fatalf("%d requests promoted the same session, want 1", ok)
	}
}

func TestOnlyAPendingSessionCanBePromoted(t *testing.T) {
	pool := testdb.New(t)
	f := create(t, pool, seedUser(t, pool, "mai@hotel.com"), session.StageFull, t0)
	if _, err := promote(pool, f.ID, t0); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("a full session was promoted: %v", err)
	}
	if _, err := lookup(pool, f.ID, t0); err != nil {
		t.Fatalf("the refused promote deleted the session: %v", err)
	}
}

func TestEndAllEndsOnlyThatUsersSessions(t *testing.T) {
	pool := testdb.New(t)
	a, b := seedUser(t, pool, "a@hotel.com"), seedUser(t, pool, "b@hotel.com")
	a1, a2, b1 := create(t, pool, a, session.StageFull, t0), create(t, pool, a, session.StagePending, t0), create(t, pool, b, session.StageFull, t0)
	if err := tx(t, pool, func(c pgx.Tx) error { return session.EndAll(ctx, c, a) }); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{a1.ID, a2.ID} {
		if _, err := lookup(pool, id, t0); !errors.Is(err, session.ErrNotFound) {
			t.Errorf("a session of user A survived: %v", err)
		}
	}
	if _, err := lookup(pool, b1.ID, t0); err != nil {
		t.Fatalf("user B's session was ended: %v", err)
	}
}

func TestIDsAreRandomAndOnlyAHashIsStored(t *testing.T) {
	pool := testdb.New(t)
	u := seedUser(t, pool, "mai@hotel.com")
	a, b := create(t, pool, u, session.StageFull, t0), create(t, pool, u, session.StageFull, t0)
	if a.ID == b.ID || len(a.ID) != 43 {
		t.Fatalf("ids %q %q: want distinct, 43 base64url characters", a.ID, b.ID)
	}
	var dump string
	if err := tx(t, pool, func(c pgx.Tx) error {
		return c.QueryRow(ctx, `select coalesce(string_agg(row_to_json(s)::text, ' '), '') from session s`).Scan(&dump)
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(dump, a.ID) || strings.Contains(dump, b.ID) {
		t.Fatal("a raw session id is stored in the database")
	}
}

func trust(t *testing.T, pool *pgxpool.Pool, u uuid.UUID, at time.Time) string {
	t.Helper()
	var tok string
	if err := tx(t, pool, func(c pgx.Tx) (e error) { tok, e = session.TrustDevice(ctx, c, u, at); return }); err != nil {
		t.Fatal(err)
	}
	return tok
}

func trusted(pool *pgxpool.Pool, u uuid.UUID, tok string, at time.Time) (ok bool) {
	db.WithoutOrg(ctx, pool, func(c pgx.Tx) error { ok = session.IsTrusted(ctx, c, u, tok, at); return nil })
	return
}

func TestTrustedDeviceLasts30Days(t *testing.T) {
	pool := testdb.New(t)
	u := seedUser(t, pool, "mai@hotel.com")
	tok := trust(t, pool, u, t0)
	if !trusted(pool, u, tok, t0.Add(29*24*time.Hour)) {
		t.Fatal("not trusted on day 29")
	}
	if trusted(pool, u, tok, t0.Add(31*24*time.Hour)) {
		t.Fatal("still trusted on day 31")
	}
}

func TestTrustedDeviceBelongsToOneUserAndOneToken(t *testing.T) {
	pool := testdb.New(t)
	a, b := seedUser(t, pool, "a@hotel.com"), seedUser(t, pool, "b@hotel.com")
	tok := trust(t, pool, a, t0)
	if trusted(pool, b, tok, t0) {
		t.Fatal("user B is trusted through user A's device")
	}
	if trusted(pool, a, "made-up-token", t0) || trusted(pool, a, "", t0) {
		t.Fatal("an unknown token is trusted")
	}
}

func TestForgetDevices(t *testing.T) {
	pool := testdb.New(t)
	a, b := seedUser(t, pool, "a@hotel.com"), seedUser(t, pool, "b@hotel.com")
	ta, tb := trust(t, pool, a, t0), trust(t, pool, b, t0)
	if err := tx(t, pool, func(c pgx.Tx) error { return session.ForgetDevices(ctx, c, a) }); err != nil {
		t.Fatal(err)
	}
	if trusted(pool, a, ta, t0) || !trusted(pool, b, tb, t0) {
		t.Fatal("ForgetDevices did not clear exactly user A's devices")
	}
}

func TestCookieAttributes(t *testing.T) {
	rec := httptest.NewRecorder()
	session.SetCookie(rec, "abc", true)
	c := rec.Result().Cookies()[0]
	if c.Name != "sid" || c.Value != "abc" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Fatalf("%+v", c)
	}
	rec = httptest.NewRecorder()
	session.SetCookie(rec, "abc", false)
	if rec.Result().Cookies()[0].Secure {
		t.Fatal("Secure set when not asked for")
	}
}

func TestReadCookie(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	if session.ReadCookie(r) != "" {
		t.Fatal("a missing cookie should read as empty")
	}
	r.AddCookie(&http.Cookie{Name: "sid", Value: "abc"})
	if session.ReadCookie(r) != "abc" {
		t.Fatal("cookie not read")
	}
}

func TestSetOrganizationIsRememberedByLookup(t *testing.T) {
	pool := testdb.New(t)
	s := create(t, pool, seedUser(t, pool, "mai@hotel.com"), session.StageFull, t0)
	if got, _ := lookup(pool, s.ID, t0); got.OrgID != nil {
		t.Fatal("a new session already has an organization")
	}
	var org uuid.UUID
	if err := tx(t, pool, func(c pgx.Tx) (e error) { org, e = tenant.CreateOrganization(ctx, c, "Sample Hotel"); return }); err != nil {
		t.Fatal(err)
	}
	if err := tx(t, pool, func(c pgx.Tx) error { return session.SetOrganization(ctx, c, s.ID, org) }); err != nil {
		t.Fatal(err)
	}
	got, err := lookup(pool, s.ID, t0.Add(time.Minute))
	if err != nil || got.OrgID == nil || *got.OrgID != org {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestSetOrganizationOnAnUnknownSessionIsNotFound(t *testing.T) {
	pool := testdb.New(t)
	err := tx(t, pool, func(c pgx.Tx) error { return session.SetOrganization(ctx, c, "nope", uuid.New()) })
	if !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestEndDeletesOnlyThatSession(t *testing.T) {
	pool := testdb.New(t)
	u := seedUser(t, pool, "mai@hotel.com")
	a, b := create(t, pool, u, session.StageFull, t0), create(t, pool, u, session.StageFull, t0)
	if err := tx(t, pool, func(c pgx.Tx) error { return session.End(ctx, c, a.ID) }); err != nil {
		t.Fatal(err)
	}
	if _, err := lookup(pool, a.ID, t0); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("ended session still known: %v", err)
	}
	if _, err := lookup(pool, b.ID, t0); err != nil {
		t.Fatalf("End removed another session: %v", err)
	}
	if err := tx(t, pool, func(c pgx.Tx) error { return session.End(ctx, c, "never-existed") }); err != nil {
		t.Fatalf("ending an unknown session should be harmless: %v", err)
	}
}

func TestClearCookieExpiresIt(t *testing.T) {
	rec := httptest.NewRecorder()
	session.ClearCookie(rec, true)
	c := rec.Result().Cookies()[0]
	if c.Name != "sid" || c.Value != "" || c.MaxAge >= 0 || !c.HttpOnly {
		t.Fatalf("%+v", c)
	}
}

func TestDeviceCookieLasts30DaysAndIsHttpOnly(t *testing.T) {
	rec := httptest.NewRecorder()
	session.SetDeviceCookie(rec, "tok", true)
	c := rec.Result().Cookies()[0]
	if c.Name != "td" || c.Value != "tok" || c.MaxAge != 30*24*3600 || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("%+v", c)
	}
	r := httptest.NewRequest("GET", "/", nil)
	if session.ReadDeviceCookie(r) != "" {
		t.Fatal("missing device cookie should read as empty")
	}
	r.AddCookie(&http.Cookie{Name: "td", Value: "tok"})
	if session.ReadDeviceCookie(r) != "tok" {
		t.Fatal("device cookie not read")
	}
	rec = httptest.NewRecorder()
	session.ClearDeviceCookie(rec, true)
	if rec.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatal("device cookie not cleared")
	}
}
