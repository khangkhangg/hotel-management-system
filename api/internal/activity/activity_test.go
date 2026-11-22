package activity_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/account"
	"hotelpms/api/internal/activity"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/db/testdb"
	"hotelpms/api/internal/httpx"
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

type world struct {
	t           *testing.T
	pool        *pgxpool.Pool
	srv         *httptest.Server
	orgA, orgB  uuid.UUID
	a1, a2, b1  uuid.UUID
	n           int
	bookingA1ID uuid.UUID
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, pool: testdb.New(t), bookingA1ID: uuid.New()}
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
	w.srv = httptest.NewServer(httpx.NewRouter(httpx.Deps{Pool: w.pool, Now: time.Now}, activity.Mount))
	t.Cleanup(w.srv.Close)
	return w
}

type person struct {
	w       *world
	id      uuid.UUID
	session string
	org     uuid.UUID
}

// member creates a signed-in person; property nil means an organization-wide owner.
func (w *world) member(org uuid.UUID, property *uuid.UUID, role string) *person {
	w.t.Helper()
	w.n++
	p := &person{w: w, org: org}
	must(w.t, db.WithoutOrg(ctx, w.pool, func(tx pgx.Tx) (e error) {
		p.id, e = account.Create(ctx, tx, fmt.Sprintf("p%d@hotel.com", w.n), "Person")
		return
	}))
	must(w.t, db.WithOrg(ctx, w.pool, org, func(tx pgx.Tx) error { return tenant.AddMembership(ctx, tx, org, p.id, property, role) }))
	must(w.t, db.WithoutOrg(ctx, w.pool, func(tx pgx.Tx) error {
		s, err := session.Create(ctx, tx, p.id, session.StageFull, "test", time.Now())
		if err != nil {
			return err
		}
		p.session = s.ID
		return session.SetOrganization(ctx, tx, s.ID, org)
	}))
	return p
}

// log records an entry as someone working in a property.
func (w *world) log(org uuid.UUID, property uuid.UUID, actor uuid.UUID, role, action, targetType string, target uuid.UUID) {
	w.t.Helper()
	c := audit.WithWho(ctx, audit.Who{UserID: actor, OrgID: org, PropertyID: property, Role: role})
	must(w.t, db.WithOrg(ctx, w.pool, org, func(tx pgx.Tx) error {
		return audit.Record(c, tx, audit.Entry{Action: action, TargetType: targetType, TargetID: target, TargetLabel: "label"})
	}))
}

type reply struct {
	status int
	body   []byte
}

func (p *person) get(path string, property *uuid.UUID) reply {
	p.w.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, p.w.srv.URL+path, nil)
	req.AddCookie(&http.Cookie{Name: "sid", Value: p.session})
	if property != nil {
		req.Header.Set("X-Property-Id", property.String())
	}
	res, err := http.DefaultClient.Do(req)
	must(p.w.t, err)
	defer res.Body.Close()
	var b []byte
	b, _ = io.ReadAll(res.Body)
	return reply{res.StatusCode, b}
}

type page struct {
	Items      []map[string]any `json:"items"`
	NextCursor *string          `json:"nextCursor"`
}

func (r reply) page(t *testing.T) page {
	t.Helper()
	var p page
	if err := json.Unmarshal(r.body, &p); err != nil || r.status != 200 {
		t.Fatalf("status %d body %s", r.status, r.body)
	}
	return p
}

func TestManagerSeesOnlyTheirOwnProperty(t *testing.T) {
	w := newWorld(t)
	owner := w.member(w.orgA, nil, "owner")
	mgr := w.member(w.orgA, &w.a1, "manager")
	w.log(w.orgA, w.a1, owner.id, "owner", "user.invited", "user", uuid.New())
	w.log(w.orgA, w.a2, owner.id, "owner", "user.invited", "user", uuid.New())
	if got := mgr.get("/api/activity", &w.a1).page(t); len(got.Items) != 1 {
		t.Fatalf("manager of a1 sees %d rows, want 1", len(got.Items))
	}
	if got := owner.get("/api/activity", nil).page(t); len(got.Items) != 2 {
		t.Fatalf("owner sees %d rows, want 2", len(got.Items))
	}
}

func TestEntriesOfAnotherOrganizationAreInvisible(t *testing.T) {
	w := newWorld(t)
	ownerB := w.member(w.orgB, nil, "owner")
	w.log(w.orgA, w.a1, ownerB.id, "owner", "user.invited", "user", uuid.New())
	if got := ownerB.get("/api/activity", nil).page(t); len(got.Items) != 0 {
		t.Fatalf("organization B sees %d rows of A", len(got.Items))
	}
}

func TestFrontDeskCannotOpenActivityButReadsTargetHistoryWithoutIP(t *testing.T) {
	w := newWorld(t)
	owner := w.member(w.orgA, nil, "owner")
	desk := w.member(w.orgA, &w.a1, "front_desk")
	w.log(w.orgA, w.a1, owner.id, "owner", "booking.moved", "booking", w.bookingA1ID)
	if r := desk.get("/api/activity", &w.a1); r.status != 403 {
		t.Fatalf("front desk /api/activity: %d", r.status)
	}
	got := desk.get("/api/activity/target/booking/"+w.bookingA1ID.String(), &w.a1).page(t)
	if len(got.Items) != 1 {
		t.Fatalf("history has %d rows", len(got.Items))
	}
	for _, key := range []string{"ip", "place"} {
		if _, has := got.Items[0][key]; has {
			t.Fatalf("target history shows %q to a front desk user", key)
		}
	}
	if got.Items[0]["action"] != "booking.moved" {
		t.Fatalf("item: %v", got.Items[0])
	}
}

func TestTargetHistoryNeedsPermissionToReadTheTarget(t *testing.T) {
	w := newWorld(t)
	desk := w.member(w.orgA, &w.a1, "front_desk")
	if r := desk.get("/api/activity/target/user/"+uuid.New().String(), &w.a1); r.status != 403 {
		t.Fatalf("front desk reading a user's history: %d", r.status)
	}
	hk := w.member(w.orgA, &w.a1, "housekeeping")
	if r := hk.get("/api/activity/target/booking/"+uuid.New().String(), &w.a1); r.status != 403 {
		t.Fatalf("housekeeping reading a booking's history: %d", r.status)
	}
}

func TestOwnerSeesIPAndPlaceAndTheRowShape(t *testing.T) {
	w := newWorld(t)
	owner := w.member(w.orgA, nil, "owner")
	target := uuid.New()
	w.log(w.orgA, w.a1, owner.id, "owner", "user.role_changed", "user", target)
	got := owner.get("/api/activity", nil).page(t)
	it := got.Items[0]
	for _, key := range []string{"id", "at", "actor", "viewAs", "action", "target", "before", "after", "reason", "device", "ip", "place", "traceId"} {
		if _, has := it[key]; !has {
			t.Fatalf("item has no %q: %v", key, it)
		}
	}
	if a := it["actor"].(map[string]any); a["id"] != owner.id.String() || a["name"] != "Person" || a["role"] != "owner" {
		t.Fatalf("actor: %v", a)
	}
	if tg := it["target"].(map[string]any); tg["type"] != "user" || tg["id"] != target.String() || tg["label"] != "label" {
		t.Fatalf("target: %v", tg)
	}
}

func TestFiltersAndPaging(t *testing.T) {
	w := newWorld(t)
	owner := w.member(w.orgA, nil, "owner")
	other := w.member(w.orgA, &w.a1, "manager")
	for i := 0; i < 5; i++ {
		w.log(w.orgA, w.a1, owner.id, "owner", "user.invited", "user", uuid.New())
	}
	w.log(w.orgA, w.a1, other.id, "manager", "user.deactivated", "user", uuid.New())

	first := owner.get("/api/activity?limit=4", nil).page(t)
	if len(first.Items) != 4 || first.NextCursor == nil {
		t.Fatalf("first page: %d items, cursor %v", len(first.Items), first.NextCursor)
	}
	if first.Items[0]["action"] != "user.deactivated" {
		t.Fatalf("newest first: got %v", first.Items[0]["action"])
	}
	second := owner.get("/api/activity?limit=4&cursor="+*first.NextCursor, nil).page(t)
	if len(second.Items) != 2 || second.NextCursor != nil {
		t.Fatalf("second page: %d items, cursor %v", len(second.Items), second.NextCursor)
	}
	if got := owner.get("/api/activity?action=user.deactivated", nil).page(t); len(got.Items) != 1 {
		t.Fatalf("by action: %d", len(got.Items))
	}
	if got := owner.get("/api/activity?user="+other.id.String(), nil).page(t); len(got.Items) != 1 {
		t.Fatalf("by user: %d", len(got.Items))
	}
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if got := owner.get("/api/activity?from="+future, nil).page(t); len(got.Items) != 0 {
		t.Fatalf("from the future: %d", len(got.Items))
	}
	for _, bad := range []string{"limit=0", "limit=abc", "cursor=x", "from=yesterday", "user=nobody"} {
		if r := owner.get("/api/activity?"+bad, nil); r.status != 400 {
			t.Fatalf("%s: %d", bad, r.status)
		}
	}
}
