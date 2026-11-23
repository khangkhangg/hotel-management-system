// Package testkit builds a running API with two organizations, signed-in people and a small HTTP client,
// so each area's tests can be written against the real routes and a real database.
package testkit

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	"hotelpms/api/internal/session"
	"hotelpms/api/internal/tenant"
)

var Ctx = context.Background()

// World is organization A (properties A1 and A2) and organization B (property B1).
type World struct {
	T          *testing.T
	Pool       *pgxpool.Pool
	Mail       *mail.Fake
	Srv        *httptest.Server
	OrgA, OrgB uuid.UUID
	A1, A2, B1 uuid.UUID
	Now        time.Time
	n          int
}

func Must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// New starts a server with the given routes. The clock is fixed at noon UTC on 2026-11-23 until moved with Advance.
func New(t *testing.T, mounts ...httpx.Mount) *World {
	t.Helper()
	w := &World{T: t, Pool: testdb.New(t), Mail: mail.NewFake(), Now: time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)}
	mk := func(name string, slugs ...string) (org uuid.UUID, props []uuid.UUID) {
		Must(t, db.WithoutOrg(Ctx, w.Pool, func(tx pgx.Tx) (e error) { org, e = tenant.CreateOrganization(Ctx, tx, name); return }))
		Must(t, db.WithOrg(Ctx, w.Pool, org, func(tx pgx.Tx) error {
			for _, s := range slugs {
				id, err := tenant.CreateProperty(Ctx, tx, org, tenant.PropertyInput{Name: s, Slug: s, TimeZone: "UTC", Currency: "USD", DefaultLocale: "en"})
				if err != nil {
					return err
				}
				props = append(props, id)
			}
			return nil
		}))
		return
	}
	a, b := []uuid.UUID{}, []uuid.UUID{}
	w.OrgA, a = mk("A", "a1", "a2")
	w.OrgB, b = mk("B", "b1")
	w.A1, w.A2, w.B1 = a[0], a[1], b[0]
	w.Srv = httptest.NewServer(httpx.NewRouter(httpx.Deps{Pool: w.Pool, Mail: w.Mail, Now: func() time.Time { return w.Now }, Background: func(f func()) { f() }}, mounts...))
	t.Cleanup(w.Srv.Close)
	return w
}

func (w *World) Advance(d time.Duration) { w.Now = w.Now.Add(d) }

// Person is a signed-in user with a session in one organization.
type Person struct {
	W       *World
	ID      uuid.UUID
	Email   string
	Session string
	Org     uuid.UUID
}

// Member creates a signed-in person; property nil means an organization-wide owner.
func (w *World) Member(org uuid.UUID, property *uuid.UUID, role string) *Person {
	w.T.Helper()
	w.n++
	p := &Person{W: w, Org: org, Email: fmt.Sprintf("p%d@hotel.com", w.n)}
	Must(w.T, db.WithoutOrg(Ctx, w.Pool, func(tx pgx.Tx) (e error) {
		p.ID, e = account.Create(Ctx, tx, p.Email, fmt.Sprintf("Person %d", w.n))
		return
	}))
	Must(w.T, db.WithOrg(Ctx, w.Pool, org, func(tx pgx.Tx) error { return tenant.AddMembership(Ctx, tx, org, p.ID, property, role) }))
	Must(w.T, db.WithoutOrg(Ctx, w.Pool, func(tx pgx.Tx) error {
		s, err := session.Create(Ctx, tx, p.ID, session.StageFull, "test", w.Now)
		if err != nil {
			return err
		}
		p.Session = s.ID
		return session.SetOrganization(Ctx, tx, s.ID, org)
	}))
	return p
}

type Reply struct {
	Status int
	Body   string
}

// Do sends a request as the person (nil means signed out). A JSON body may be given for methods that change things.
func (p *Person) Do(method, path string, property *uuid.UUID, body string) Reply {
	p.W.T.Helper()
	return do(p.W, p, method, path, property, body)
}

// Anon sends a request with no session.
func (w *World) Anon(method, path, body string) Reply { return do(w, nil, method, path, nil, body) }

func do(w *World, p *Person, method, path string, property *uuid.UUID, body string) Reply {
	w.T.Helper()
	req, err := http.NewRequest(method, w.Srv.URL+path, strings.NewReader(body))
	Must(w.T, err)
	if p != nil {
		req.AddCookie(&http.Cookie{Name: "sid", Value: p.Session})
	}
	if property != nil {
		req.Header.Set("X-Property-Id", property.String())
	}
	if method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", "t")
		req.AddCookie(&http.Cookie{Name: "csrf", Value: "t"})
	}
	res, err := http.DefaultClient.Do(req)
	Must(w.T, err)
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return Reply{res.StatusCode, string(b)}
}

// Want fails the test unless the status matches and the body contains each text.
func (r Reply) Want(t *testing.T, status int, contains ...string) Reply {
	t.Helper()
	if r.Status != status {
		t.Fatalf("status %d, want %d; body %s", r.Status, status, r.Body)
	}
	for _, c := range contains {
		if !strings.Contains(r.Body, c) {
			t.Fatalf("body %s does not contain %q", r.Body, c)
		}
	}
	return r
}
