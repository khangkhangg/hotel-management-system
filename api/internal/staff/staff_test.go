package staff_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	"hotelpms/api/internal/session"
	"hotelpms/api/internal/signin"
	"hotelpms/api/internal/staff"
	"hotelpms/api/internal/tenant"
)

var ctx = context.Background()

const goodPassword = "twelve chars ok"

type fakeBreach struct{ breached map[string]bool }

func (f fakeBreach) Breached(_ context.Context, p string) (bool, error) { return f.breached[p], nil }

var _ password.BreachChecker = fakeBreach{}

// world: organization "A" with properties a1 and a2, organization "B" with property b1.
type world struct {
	t          *testing.T
	pool       *pgxpool.Pool
	mail       *mail.Fake
	srv        *httptest.Server
	mu         sync.Mutex
	now        time.Time
	breach     fakeBreach
	orgA, orgB uuid.UUID
	a1, a2, b1 uuid.UUID
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, pool: testdb.New(t), mail: mail.NewFake(), now: time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC), breach: fakeBreach{breached: map[string]bool{"correct horse battery": true}}}
	mk := func(name string, slugs ...string) (org uuid.UUID, props []uuid.UUID) {
		must(t, db.WithoutOrg(ctx, w.pool, func(tx pgx.Tx) (e error) { org, e = tenant.CreateOrganization(ctx, tx, name); return }))
		must(t, db.WithOrg(ctx, w.pool, org, func(tx pgx.Tx) error {
			for _, s := range slugs {
				id, err := tenant.CreateProperty(ctx, tx, org, tenant.PropertyInput{Name: s, Slug: s, TimeZone: "UTC", Currency: "USD", DefaultLocale: "en"})
				if err != nil {
					return err
				}
				props = append(props, id)
			}
			return nil
		}))
		return
	}
	var a, b []uuid.UUID
	w.orgA, a = mk("A", "a1", "a2")
	w.orgB, b = mk("B", "b1")
	w.a1, w.a2, w.b1 = a[0], a[1], b[0]
	deps := httpx.Deps{Pool: w.pool, Mail: w.mail, Now: w.clock, BaseURL: "https://app.example.com", Passwords: w.breach}
	w.srv = httptest.NewServer(httpx.NewRouter(deps, staff.Mount, signin.Mount))
	t.Cleanup(w.srv.Close)
	return w
}

func (w *world) clock() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.now
}

func (w *world) advance(d time.Duration) {
	w.mu.Lock()
	w.now = w.now.Add(d)
	w.mu.Unlock()
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func ptr(id uuid.UUID) *uuid.UUID { return &id }

// ---------- people ----------

type person struct {
	w       *world
	id      uuid.UUID
	email   string
	session string
	org     uuid.UUID
}

// member creates a user (with a password) who works in org at property (nil = owner) and has a full session there.
func (w *world) member(email string, org uuid.UUID, property *uuid.UUID, role string) *person {
	w.t.Helper()
	hash, err := password.Hash(goodPassword)
	must(w.t, err)
	p := &person{w: w, email: email, org: org}
	must(w.t, db.WithoutOrg(ctx, w.pool, func(tx pgx.Tx) (e error) {
		if p.id, e = account.Create(ctx, tx, email, "Test Person"); e != nil {
			return
		}
		return account.SetPassword(ctx, tx, p.id, hash)
	}))
	w.join(p, org, property, role)
	p.session = w.newSession(p, org)
	return p
}

func (w *world) join(p *person, org uuid.UUID, property *uuid.UUID, role string) {
	w.t.Helper()
	must(w.t, db.WithOrg(ctx, w.pool, org, func(tx pgx.Tx) error { return tenant.AddMembership(ctx, tx, org, p.id, property, role) }))
}

func (w *world) newSession(p *person, org uuid.UUID) (id string) {
	w.t.Helper()
	must(w.t, db.WithoutOrg(ctx, w.pool, func(tx pgx.Tx) error {
		s, e := session.Create(ctx, tx, p.id, session.StageFull, "test", w.clock())
		if e != nil {
			return e
		}
		id = s.ID
		return session.SetOrganization(ctx, tx, s.ID, org)
	}))
	return
}

type reply struct {
	status  int
	body    string
	cookies []*http.Cookie
}

func (r reply) want(t *testing.T, status int, contains ...string) reply {
	t.Helper()
	if r.status != status {
		t.Fatalf("status %d, want %d; body %s", r.status, status, r.body)
	}
	for _, c := range contains {
		if !strings.Contains(r.body, c) {
			t.Fatalf("body %s does not contain %q", r.body, c)
		}
	}
	return r
}

func (r reply) cookie(name string) string {
	for _, c := range r.cookies {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

// do sends a request as this person. A nil person is somebody without a session.
func (w *world) do(p *person, method, path string, property *uuid.UUID, body string) reply {
	req, err := http.NewRequest(method, w.srv.URL+path, strings.NewReader(body))
	must(w.t, err)
	if p != nil && p.session != "" {
		req.AddCookie(&http.Cookie{Name: "sid", Value: p.session})
	}
	if property != nil {
		req.Header.Set("X-Property-Id", property.String())
	}
	if method != "GET" {
		req.Header.Set("X-CSRF-Token", "t")
		req.AddCookie(&http.Cookie{Name: "csrf", Value: "t"})
	}
	resp, err := http.DefaultClient.Do(req)
	must(w.t, err)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return reply{status: resp.StatusCode, body: string(raw), cookies: resp.Cookies()}
}

// withCookie is do, but with an explicit session cookie (for a person who just accepted an invite).
func (w *world) asSession(id string) *person { return &person{w: w, session: id} }

var linkToken = regexp.MustCompile(`https://app\.example\.com/(?:invite|reset)/([A-Za-z0-9_-]+)`)

func (w *world) lastLinkToken() string {
	w.t.Helper()
	sent := w.mail.Sent()
	if len(sent) == 0 {
		w.t.Fatal("no email was sent")
	}
	m := linkToken.FindStringSubmatch(sent[len(sent)-1].Text)
	if m == nil {
		w.t.Fatalf("no link in %q", sent[len(sent)-1].Text)
	}
	return m[1]
}

func (w *world) memberships(email string) (out []struct {
	Property *uuid.UUID
	Role     string
	Status   string
}) {
	w.t.Helper()
	for _, org := range []uuid.UUID{w.orgA, w.orgB} {
		must(w.t, db.WithOrg(ctx, w.pool, org, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `select m.property_id, m.role, m.status from membership m join app_user u on u.id = m.user_id where u.email = $1 order by m.role`, email)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var row struct {
					Property *uuid.UUID
					Role     string
					Status   string
				}
				if err := rows.Scan(&row.Property, &row.Role, &row.Status); err != nil {
					return err
				}
				out = append(out, row)
			}
			return rows.Err()
		}))
	}
	return
}

func jsonOf(t *testing.T, r reply, v any) {
	t.Helper()
	must(t, json.Unmarshal([]byte(r.body), v))
}

var regexp6 = regexp.MustCompile(`\b(\d{6})\b`)
