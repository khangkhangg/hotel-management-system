package notify_test

import (
	"bufio"
	"context"
	"encoding/json"
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

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/account"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/db/testdb"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/mail"
	"hotelpms/api/internal/notify"
	"hotelpms/api/internal/session"
	"hotelpms/api/internal/tenant"
)

var ctx = context.Background()

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type app struct {
	t          *testing.T
	pool       *pgxpool.Pool
	mail       *mail.Fake
	srv        *httptest.Server
	orgA, orgB uuid.UUID
	a1, a2, b1 uuid.UUID
	now        time.Time
	n          int
}

func newApp(t *testing.T) *app {
	t.Helper()
	a := &app{t: t, pool: testdb.New(t), mail: mail.NewFake(), now: time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)}
	mk := func(name string, slugs ...string) (org uuid.UUID, props []uuid.UUID) {
		must(t, db.WithoutOrg(ctx, a.pool, func(tx pgx.Tx) (e error) { org, e = tenant.CreateOrganization(ctx, tx, name); return }))
		must(t, db.WithOrg(ctx, a.pool, org, func(tx pgx.Tx) error {
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
	var pa, pb []uuid.UUID
	a.orgA, pa = mk("A", "a1", "a2")
	a.orgB, pb = mk("B", "b1")
	a.a1, a.a2, a.b1 = pa[0], pa[1], pb[0]
	hub, err := notify.NewHub(a.pool)
	must(t, err)
	t.Cleanup(hub.Close) // registered after the pool's cleanup, so it runs before it
	a.srv = httptest.NewServer(httpx.NewRouter(httpx.Deps{Pool: a.pool, Mail: a.mail, Now: func() time.Time { return a.now }}, notify.Mount(hub)))
	t.Cleanup(a.srv.Close)
	return a
}

type person struct {
	a       *app
	id      uuid.UUID
	email   string
	session string
}

// person creates a signed-in user; property nil means an organization-wide owner.
func (a *app) person(org uuid.UUID, property *uuid.UUID, role access.Role) *person {
	a.t.Helper()
	a.n++
	p := &person{a: a, email: fmt.Sprintf("p%d@hotel.com", a.n)}
	must(a.t, db.WithoutOrg(ctx, a.pool, func(tx pgx.Tx) (e error) { p.id, e = account.Create(ctx, tx, p.email, "Person"); return }))
	must(a.t, db.WithOrg(ctx, a.pool, org, func(tx pgx.Tx) error { return tenant.AddMembership(ctx, tx, org, p.id, property, string(role)) }))
	must(a.t, db.WithoutOrg(ctx, a.pool, func(tx pgx.Tx) error {
		s, err := session.Create(ctx, tx, p.id, session.StageFull, "test", a.now)
		if err != nil {
			return err
		}
		p.session = s.ID
		return session.SetOrganization(ctx, tx, s.ID, org)
	}))
	return p
}

// send runs notify.Send in a transaction, then delivers any emails that are due.
func (a *app) send(in notify.Input) {
	a.t.Helper()
	if in.OrgID == uuid.Nil {
		in.OrgID = a.orgA
	}
	if in.PropertyID == uuid.Nil {
		in.PropertyID = a.a1
	}
	in.Now = a.now
	must(a.t, db.WithOrg(ctx, a.pool, in.OrgID, func(tx pgx.Tx) error { return notify.Send(ctx, tx, in) }))
	a.deliver()
}

func (a *app) deliver() {
	a.t.Helper()
	must(a.t, notify.Deliver(ctx, a.pool, a.mail, a.now))
}

func (a *app) advance(d time.Duration) { a.now = a.now.Add(d) }

type item struct {
	ID         string  `json:"id"`
	Level      string  `json:"level"`
	Key        string  `json:"key"`
	Title      string  `json:"title"`
	Body       string  `json:"body"`
	ActionURL  string  `json:"actionURL"`
	Count      int     `json:"count"`
	OpenedAt   *string `json:"openedAt"`
	ResolvedAt *string `json:"resolvedAt"`
	ResolvedBy *string `json:"resolvedBy"`
}

type list struct {
	Unread int    `json:"unread"`
	Items  []item `json:"items"`
}

func (p *person) call(method, path, body string) (int, []byte) {
	p.a.t.Helper()
	req, _ := http.NewRequest(method, p.a.srv.URL+path, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "sid", Value: p.session})
	if method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", "t")
		req.AddCookie(&http.Cookie{Name: "csrf", Value: "t"})
	}
	res, err := http.DefaultClient.Do(req)
	must(p.a.t, err)
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func (p *person) notifications() list {
	p.a.t.Helper()
	status, b := p.call("GET", "/api/notifications", "")
	var l list
	if err := json.Unmarshal(b, &l); err != nil || status != 200 {
		p.a.t.Fatalf("status %d body %s", status, b)
	}
	return l
}

func TestSameKeyUpdatesOneNotification(t *testing.T) {
	a := newApp(t)
	mgr := a.person(a.orgA, &a.a1, access.Manager)
	for i := 0; i < 20; i++ {
		a.send(notify.Input{ToRole: access.Manager, Level: notify.Urgent, Key: "channel-down:expedia", Title: "Expedia is not confirming updates"})
	}
	got := mgr.notifications()
	if len(got.Items) != 1 || got.Items[0].Count != 20 || got.Unread != 1 {
		t.Fatalf("%+v", got)
	}
	if n := len(a.mail.Sent()); n != 1 {
		t.Fatalf("%d emails, want 1", n)
	}
}

func TestOnlyTargetRoleAtThatPropertyReceives(t *testing.T) {
	a := newApp(t)
	target := a.person(a.orgA, &a.a1, access.Manager)
	others := []*person{
		a.person(a.orgB, &a.b1, access.Manager),   // manager of another hotel
		a.person(a.orgA, &a.a1, access.FrontDesk), // other role, same hotel
		a.person(a.orgA, &a.a2, access.Manager),   // same role, other property
	}
	a.send(notify.Input{ToRole: access.Manager, Level: notify.Info, Key: "k", Title: "t"})
	if n := len(target.notifications().Items); n != 1 {
		t.Fatalf("target got %d", n)
	}
	for i, o := range others {
		if n := len(o.notifications().Items); n != 0 {
			t.Fatalf("person %d got %d notifications", i, n)
		}
	}
}

func TestOwnersReceiveForEveryProperty(t *testing.T) {
	a := newApp(t)
	owner := a.person(a.orgA, nil, access.Owner)
	a.send(notify.Input{PropertyID: a.a2, ToRole: access.Owner, Level: notify.Info, Key: "k", Title: "t"})
	if n := len(owner.notifications().Items); n != 1 {
		t.Fatalf("owner got %d", n)
	}
}

func TestADeactivatedPersonReceivesNothing(t *testing.T) {
	a := newApp(t)
	mgr := a.person(a.orgA, &a.a1, access.Manager)
	must(t, db.WithOrg(ctx, a.pool, a.orgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update membership set status = 'deactivated' where user_id = $1`, mgr.id)
		return err
	}))
	a.send(notify.Input{ToRole: access.Manager, Level: notify.Info, Key: "k", Title: "t"})
	if n := len(mgr.notifications().Items); n != 0 {
		t.Fatalf("got %d", n)
	}
}

func TestEscalation(t *testing.T) {
	setup := func(t *testing.T) (*app, *person, *person, *person) {
		a := newApp(t)
		desk := a.person(a.orgA, &a.a1, access.FrontDesk)
		mgr := a.person(a.orgA, &a.a1, access.Manager)
		owner := a.person(a.orgA, nil, access.Owner)
		deskID := desk.id
		a.send(notify.Input{ToUser: &deskID, Level: notify.Urgent, Key: "overbooked:1", Title: "Overbooked tonight"})
		return a, desk, mgr, owner
	}
	escalate := func(a *app) {
		a.t.Helper()
		must(a.t, notify.Escalate(ctx, a.pool, a.now))
		a.deliver()
	}
	t.Run("a manager at ten minutes and the owner at thirty", func(t *testing.T) {
		a, _, mgr, owner := setup(t)
		a.advance(9 * time.Minute)
		escalate(a)
		if len(mgr.notifications().Items) != 0 {
			t.Fatal("the manager was told after 9 minutes")
		}
		a.advance(time.Minute)
		escalate(a)
		if got := mgr.notifications(); len(got.Items) != 1 || len(owner.notifications().Items) != 0 {
			t.Fatalf("at 10 minutes: manager %d, owner %d", len(got.Items), len(owner.notifications().Items))
		}
		emails := len(a.mail.Sent())
		a.advance(20 * time.Minute)
		escalate(a)
		if len(owner.notifications().Items) != 1 {
			t.Fatal("the owner was not told at 30 minutes")
		}
		escalate(a) // running again changes nothing
		if got := mgr.notifications().Items[0].Count; got != 1 || len(a.mail.Sent()) != emails+1 {
			t.Fatalf("repeat sweep: count %d, emails %d (was %d)", got, len(a.mail.Sent()), emails)
		}
	})
	t.Run("opening it stops both steps", func(t *testing.T) {
		a, desk, mgr, owner := setup(t)
		a.advance(5 * time.Minute)
		status, _ := desk.call("POST", "/api/notifications/"+desk.notifications().Items[0].ID+"/open", "")
		if status != 204 {
			t.Fatalf("open: %d", status)
		}
		a.advance(40 * time.Minute)
		escalate(a)
		if len(mgr.notifications().Items)+len(owner.notifications().Items) != 0 {
			t.Fatal("an opened notification was escalated")
		}
	})
	t.Run("a manager opening it stops the owner step", func(t *testing.T) {
		a, _, mgr, owner := setup(t)
		a.advance(15 * time.Minute)
		escalate(a)
		if status, _ := mgr.call("POST", "/api/notifications/"+mgr.notifications().Items[0].ID+"/open", ""); status != 204 {
			t.Fatalf("open: %d", status)
		}
		a.advance(20 * time.Minute)
		escalate(a)
		if len(owner.notifications().Items) != 0 {
			t.Fatal("the owner was told although a manager had opened it")
		}
	})
	t.Run("resolving it stops both steps", func(t *testing.T) {
		a, _, mgr, owner := setup(t)
		must(t, db.WithOrg(ctx, a.pool, a.orgA, func(tx pgx.Tx) error { return notify.Resolve(ctx, tx, a.orgA, "overbooked:1", mgr.id) }))
		a.advance(40 * time.Minute)
		escalate(a)
		if len(mgr.notifications().Items)+len(owner.notifications().Items) != 0 {
			t.Fatal("a resolved notification was escalated")
		}
	})
	t.Run("action-needed notifications are not escalated", func(t *testing.T) {
		a := newApp(t)
		desk := a.person(a.orgA, &a.a1, access.FrontDesk)
		mgr := a.person(a.orgA, &a.a1, access.Manager)
		id := desk.id
		a.send(notify.Input{ToUser: &id, Level: notify.ActionNeeded, Key: "k", Title: "t"})
		a.advance(time.Hour)
		escalate(a)
		if len(mgr.notifications().Items) != 0 {
			t.Fatal("escalated")
		}
	})
}

func TestQuietHoursHoldActionNeededEmailButNotUrgent(t *testing.T) {
	a := newApp(t)
	a.now = time.Date(2026, 11, 23, 23, 0, 0, 0, time.UTC)
	mgr := a.person(a.orgA, &a.a1, access.Manager)
	if status, b := mgr.call("PUT", "/api/notification-preferences", `{"quietFrom":"22:00","quietTo":"07:00","email":{"urgent":true,"actionNeeded":true}}`); status != 204 {
		t.Fatalf("preferences: %d %s", status, b)
	}
	a.send(notify.Input{ToRole: access.Manager, Level: notify.ActionNeeded, Key: "k1", Title: "Approve refund"})
	if n := len(a.mail.Sent()); n != 0 {
		t.Fatalf("%d emails during quiet hours", n)
	}
	a.send(notify.Input{ToRole: access.Manager, Level: notify.Urgent, Key: "k2", Title: "Overbooked"})
	if n := len(a.mail.Sent()); n != 1 || !strings.Contains(a.mail.Sent()[0].Subject, "Overbooked") {
		t.Fatalf("urgent mail during quiet hours: %+v", a.mail.Sent())
	}
	a.now = time.Date(2026, 11, 24, 6, 59, 0, 0, time.UTC)
	a.deliver()
	if n := len(a.mail.Sent()); n != 1 {
		t.Fatalf("%d emails at 06:59", n)
	}
	a.now = time.Date(2026, 11, 24, 7, 0, 0, 0, time.UTC)
	a.deliver()
	a.deliver() // a second sweep must not send it again
	if n := len(a.mail.Sent()); n != 2 || !strings.Contains(a.mail.Sent()[1].Subject, "Approve refund") {
		t.Fatalf("at 07:00: %+v", a.mail.Sent())
	}
}

func TestEmailFollowsThePersonsChoice(t *testing.T) {
	a := newApp(t)
	mgr := a.person(a.orgA, &a.a1, access.Manager)
	a.send(notify.Input{ToRole: access.Manager, Level: notify.ActionNeeded, Key: "k1", Title: "no email by default"})
	a.send(notify.Input{ToRole: access.Manager, Level: notify.Info, Key: "k2", Title: "info is in-app only"})
	if n := len(a.mail.Sent()); n != 0 {
		t.Fatalf("%d emails", n)
	}
	mgr.call("PUT", "/api/notification-preferences", `{"quietFrom":null,"quietTo":null,"email":{"urgent":false,"actionNeeded":true}}`)
	a.send(notify.Input{ToRole: access.Manager, Level: notify.ActionNeeded, Key: "k3", Title: "now by email"})
	a.send(notify.Input{ToRole: access.Manager, Level: notify.Urgent, Key: "k4", Title: "urgent but switched off"})
	if sent := a.mail.Sent(); len(sent) != 1 || !strings.Contains(sent[0].Subject, "now by email") || sent[0].To != mgr.email {
		t.Fatalf("%+v", sent)
	}
	if got := mgr.notifications(); len(got.Items) != 4 {
		t.Fatalf("in-app count %d, want 4", len(got.Items))
	}
}

func TestResolveMarksEveryRecipient(t *testing.T) {
	a := newApp(t)
	m1, m2 := a.person(a.orgA, &a.a1, access.Manager), a.person(a.orgA, &a.a1, access.Manager)
	a.send(notify.Input{ToRole: access.Manager, Level: notify.ActionNeeded, Key: "refund:9", Title: "Approve refund"})
	must(t, db.WithOrg(ctx, a.pool, a.orgA, func(tx pgx.Tx) error { return notify.Resolve(ctx, tx, a.orgA, "refund:9", m1.id) }))
	for _, p := range []*person{m1, m2} {
		got := p.notifications()
		if got.Unread != 0 || got.Items[0].ResolvedAt == nil || got.Items[0].ResolvedBy == nil || *got.Items[0].ResolvedBy != m1.id.String() {
			t.Fatalf("%+v", got)
		}
	}
	// After it is resolved, the same key starts a fresh notification.
	a.send(notify.Input{ToRole: access.Manager, Level: notify.ActionNeeded, Key: "refund:9", Title: "Approve refund"})
	if got := m1.notifications(); len(got.Items) != 2 || got.Unread != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestOpenAndOpenAll(t *testing.T) {
	a := newApp(t)
	mgr, other := a.person(a.orgA, &a.a1, access.Manager), a.person(a.orgA, &a.a1, access.Manager)
	a.send(notify.Input{ToRole: access.Manager, Level: notify.Info, Key: "k1", Title: "one"})
	a.send(notify.Input{ToRole: access.Manager, Level: notify.Info, Key: "k2", Title: "two"})
	first := mgr.notifications().Items[0].ID
	if status, _ := other.call("POST", "/api/notifications/"+first+"/open", ""); status != 404 {
		// the other manager has their own copy with another id, so this id is not theirs
		t.Fatalf("opening someone else's: %d", status)
	}
	if status, _ := mgr.call("POST", "/api/notifications/"+first+"/open", ""); status != 204 || mgr.notifications().Unread != 1 {
		t.Fatalf("open: %d, unread %d", status, mgr.notifications().Unread)
	}
	if status, _ := mgr.call("POST", "/api/notifications/open-all", ""); status != 204 || mgr.notifications().Unread != 0 {
		t.Fatalf("open-all: %d, unread %d", status, mgr.notifications().Unread)
	}
	if other.notifications().Unread != 2 {
		t.Fatal("opening changed another person's notifications")
	}
	if status, _ := mgr.call("POST", "/api/notifications/not-an-id/open", ""); status != 404 {
		t.Fatalf("bad id: %d", status)
	}
}

func TestPreferencesRoundTripAndValidation(t *testing.T) {
	a := newApp(t)
	p := a.person(a.orgA, &a.a1, access.Manager)
	status, b := p.call("GET", "/api/notification-preferences", "")
	if status != 200 || !strings.Contains(string(b), `"urgent":true`) || !strings.Contains(string(b), `"actionNeeded":false`) || !strings.Contains(string(b), `"quietFrom":null`) {
		t.Fatalf("defaults: %d %s", status, b)
	}
	if status, _ := p.call("PUT", "/api/notification-preferences", `{"quietFrom":"22:00","quietTo":"07:30","email":{"urgent":true,"actionNeeded":true}}`); status != 204 {
		t.Fatalf("put: %d", status)
	}
	if _, b := p.call("GET", "/api/notification-preferences", ""); !strings.Contains(string(b), `"quietTo":"07:30"`) || !strings.Contains(string(b), `"actionNeeded":true`) {
		t.Fatalf("saved: %s", b)
	}
	for _, bad := range []string{`{"quietFrom":"25:00","quietTo":"07:00","email":{}}`, `{"quietFrom":"22:00","quietTo":null,"email":{}}`, `{"quietFrom":"ten","quietTo":"07:00","email":{}}`, `{not json}`} {
		if status, _ := p.call("PUT", "/api/notification-preferences", bad); status != 400 {
			t.Fatalf("%s: %d", bad, status)
		}
	}
}

func TestNotificationRoutesNeedASession(t *testing.T) {
	a := newApp(t)
	anon := &person{a: a}
	for _, path := range []string{"/api/notifications", "/api/notification-preferences", "/api/notifications/stream"} {
		if status, _ := anon.call("GET", path, ""); status != 401 {
			t.Fatalf("%s: %d", path, status)
		}
	}
}

// openStream returns a channel of event names received on the person's stream.
func (p *person) openStream() (events <-chan string, closeStream func()) {
	p.a.t.Helper()
	c, cancel := context.WithCancel(ctx)
	req, _ := http.NewRequestWithContext(c, "GET", p.a.srv.URL+"/api/notifications/stream", nil)
	req.AddCookie(&http.Cookie{Name: "sid", Value: p.session})
	res, err := http.DefaultClient.Do(req)
	must(p.a.t, err)
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		p.a.t.Fatalf("stream: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	out := make(chan string, 16)
	go func() {
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if name, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
				out <- name
			}
		}
	}()
	return out, cancel
}

func TestStreamDeliversWithoutReload(t *testing.T) {
	a := newApp(t)
	mgr := a.person(a.orgA, &a.a1, access.Manager)
	events, closeStream := mgr.openStream()
	defer closeStream()
	a.send(notify.Input{ToRole: access.Manager, Level: notify.Info, Key: "k", Title: "t"})
	select {
	case name := <-events:
		if name != "notification" {
			t.Fatalf("event %q", name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event within 2 seconds")
	}
}

func TestStreamIsPerUser(t *testing.T) {
	a := newApp(t)
	target := a.person(a.orgA, &a.a1, access.Manager)
	bystander := a.person(a.orgA, &a.a2, access.Manager)
	mine, closeMine := target.openStream()
	theirs, closeTheirs := bystander.openStream()
	defer closeMine()
	defer closeTheirs()
	a.send(notify.Input{ToRole: access.Manager, Level: notify.Info, Key: "k", Title: "t"})
	select {
	case <-mine:
	case <-time.After(2 * time.Second):
		t.Fatal("the recipient got no event")
	}
	select {
	case name := <-theirs:
		t.Fatalf("another person's stream received %q", name)
	case <-time.After(500 * time.Millisecond):
	}
}
