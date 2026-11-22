package access_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
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
	"hotelpms/api/internal/session"
	"hotelpms/api/internal/tenant"
)

var ctx = context.Background()

// ---------- the matrix: spec 01, copied cell by cell ----------

func TestMatrixMatchesSpec(t *testing.T) {
	O, M, F, R, H := access.Owner, access.Manager, access.FrontDesk, access.Revenue, access.Housekeeping
	yes := map[access.Permission][]access.Role{
		"reservations.view":    {O, M, F, R},
		"reservations.edit":    {O, M, F},
		"checkin":              {O, M, F},
		"guests.contact.view":  {O, M, F},
		"payments.take":        {O, M, F},
		"refunds.request":      {O, M, F},
		"refunds.approve":      {O, M},
		"rates.edit":           {O, M, R},
		"channels.manage":      {O, M, R},
		"housekeeping.view":    {O, M, F, H},
		"housekeeping.update":  {O, M, H},
		"extras.manage":        {O, M},
		"reports.view":         {O, M},
		"reports.revenue.view": {O, M, R},
		"users.manage":         {O, M},
		"owners.manage":        {O},
		"payments.configure":   {O},
		"branding.configure":   {O},
		"audit.view":           {O, M},
	}
	for p, roles := range yes {
		for _, r := range access.AllRoles {
			if got, want := access.Can(r, p), slices.Contains(roles, r); got != want {
				t.Errorf("%s %s: got %v want %v", r, p, got, want)
			}
		}
	}
	if len(yes) != len(access.AllPermissions) {
		t.Fatalf("this test has %d rows but there are %d permissions", len(yes), len(access.AllPermissions))
	}
	for _, p := range access.AllPermissions {
		if _, ok := yes[p]; !ok {
			t.Errorf("permission %q has no row in this test", p)
		}
	}
}

func TestFiveRolesExactly(t *testing.T) {
	want := []access.Role{"owner", "manager", "front_desk", "revenue", "housekeeping"}
	if !slices.Equal(access.AllRoles, want) {
		t.Fatalf("%v", access.AllRoles)
	}
}

func TestUnknownRoleOrPermissionCanDoNothing(t *testing.T) {
	if access.Can("janitor", "reservations.view") || access.Can(access.Owner, "made.up") || access.Can("", "") {
		t.Fatal("an unknown role or permission was allowed")
	}
}

func TestHousekeepingNeverSeesGuestsOrMoney(t *testing.T) {
	for _, p := range []access.Permission{"guests.contact.view", "payments.take", "refunds.request", "reservations.view", "rates.edit", "reports.view"} {
		if access.Can(access.Housekeeping, p) {
			t.Errorf("housekeeping can %s", p)
		}
	}
}

// ---------- the middleware ----------

type world struct {
	pool           *pgxpool.Pool
	srv            *httptest.Server
	orgA, orgB     uuid.UUID
	propA1, propA2 uuid.UUID // two properties of organization A
	propB          uuid.UUID
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{pool: testdb.New(t)}
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
	w.propA1, w.propA2, w.propB = a[0], a[1], b[0]

	d := httpx.Deps{Pool: w.pool, Now: func() time.Time { return time.Now() }}
	echo := func(mux *http.ServeMux, d httpx.Deps) {
		for _, p := range access.AllPermissions {
			p := p
			mux.Handle("/api/test/"+string(p), access.Require(d, p)(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				httpx.JSON(rw, 200, access.ActorFrom(r.Context()))
			})))
		}
	}
	w.srv = httptest.NewServer(httpx.NewRouter(d, access.Mount, echo))
	t.Cleanup(w.srv.Close)
	return w
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type person struct {
	w       *world
	id      uuid.UUID
	session string
}

// signedIn makes a user with a full session in org (nil org = none chosen). Memberships are added separately.
func (w *world) signedIn(t *testing.T, email string, org *uuid.UUID) *person {
	t.Helper()
	p := &person{w: w}
	must(t, db.WithoutOrg(ctx, w.pool, func(tx pgx.Tx) error {
		var e error
		if p.id, e = account.Create(ctx, tx, email, "Test"); e != nil {
			return e
		}
		s, e := session.Create(ctx, tx, p.id, session.StageFull, "test", time.Now())
		if e != nil {
			return e
		}
		p.session = s.ID
		if org != nil {
			return session.SetOrganization(ctx, tx, s.ID, *org)
		}
		return nil
	}))
	return p
}

func (p *person) member(t *testing.T, org uuid.UUID, property *uuid.UUID, role string) *person {
	t.Helper()
	must(t, db.WithOrg(ctx, p.w.pool, org, func(tx pgx.Tx) error { return tenant.AddMembership(ctx, tx, org, p.id, property, role) }))
	return p
}

type reply struct {
	status int
	body   string
}

func (p *person) do(method, path string, property *uuid.UUID, body string) reply {
	req, _ := http.NewRequest(method, p.w.srv.URL+path, strings.NewReader(body))
	if p.session != "" {
		req.AddCookie(&http.Cookie{Name: "sid", Value: p.session})
	}
	if property != nil {
		req.Header.Set("X-Property-Id", property.String())
	}
	if method != "GET" { // the router wants a CSRF token on writes
		req.Header.Set("X-CSRF-Token", "t")
		req.AddCookie(&http.Cookie{Name: "csrf", Value: "t"})
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return reply{resp.StatusCode, string(raw)}
}

func (p *person) can(perm access.Permission, property *uuid.UUID) reply {
	return p.do("GET", "/api/test/"+string(perm), property, "")
}

func ptr(id uuid.UUID) *uuid.UUID { return &id }

func TestRolePerProperty(t *testing.T) {
	w := newWorld(t)
	p := w.signedIn(t, "mai@hotel.com", &w.orgA).member(t, w.orgA, &w.propA1, "front_desk").member(t, w.orgA, &w.propA2, "manager")
	if r := p.can("users.manage", ptr(w.propA1)); r.status != 403 {
		t.Fatalf("front desk at A1 may manage users: %d", r.status)
	}
	if r := p.can("users.manage", ptr(w.propA2)); r.status != 200 {
		t.Fatalf("manager at A2 may not manage users: %d %s", r.status, r.body)
	}
	if r := p.can("checkin", ptr(w.propA1)); r.status != 200 {
		t.Fatalf("front desk at A1 may not check in: %d", r.status)
	}
}

func TestActorCarriesWhoAndWhere(t *testing.T) {
	w := newWorld(t)
	p := w.signedIn(t, "mai@hotel.com", &w.orgA).member(t, w.orgA, &w.propA1, "front_desk")
	var a access.Actor
	r := p.can("checkin", ptr(w.propA1))
	must(t, json.Unmarshal([]byte(r.body), &a))
	if a.UserID != p.id || a.OrgID != w.orgA || a.PropertyID != w.propA1 || a.Role != access.FrontDesk || a.ViewAs != "" {
		t.Fatalf("%+v", a)
	}
}

func TestOwnerWorksAtEveryPropertyOfTheirOrganization(t *testing.T) {
	w := newWorld(t)
	p := w.signedIn(t, "linh@hotel.com", &w.orgA).member(t, w.orgA, nil, "owner")
	for _, prop := range []uuid.UUID{w.propA1, w.propA2} {
		if r := p.can("payments.configure", &prop); r.status != 200 || !strings.Contains(r.body, `"owner"`) {
			t.Fatalf("owner at %s: %d %s", prop, r.status, r.body)
		}
	}
	if r := p.can("payments.configure", ptr(w.propB)); r.status != 403 {
		t.Fatalf("the owner of A reached a property of B: %d", r.status)
	}
}

func TestWithoutAPropertyOnlyOrganizationWideRolesQualify(t *testing.T) {
	w := newWorld(t)
	owner := w.signedIn(t, "linh@hotel.com", &w.orgA).member(t, w.orgA, nil, "owner")
	mgr := w.signedIn(t, "daniel@hotel.com", &w.orgA).member(t, w.orgA, &w.propA1, "manager")
	if r := owner.can("branding.configure", nil); r.status != 200 {
		t.Fatalf("owner without a property header: %d", r.status)
	}
	if r := mgr.can("users.manage", nil); r.status != 403 {
		t.Fatalf("a property manager without a property header was allowed: %d", r.status)
	}
}

func TestViewAsDoesNotRemoveOwnerRights(t *testing.T) {
	w := newWorld(t)
	p := w.signedIn(t, "linh@hotel.com", &w.orgA).member(t, w.orgA, nil, "owner")
	must(t, db.WithoutOrg(ctx, w.pool, func(tx pgx.Tx) error { return session.SetViewAs(ctx, tx, p.session, "front_desk") }))
	var a access.Actor
	r := p.can("payments.configure", ptr(w.propA1))
	if r.status != 200 {
		t.Fatalf("viewing as front desk took owner rights away: %d", r.status)
	}
	must(t, json.Unmarshal([]byte(r.body), &a))
	if a.Role != access.Owner || a.ViewAs != "front_desk" {
		t.Fatalf("%+v: the actor must be the real owner with the view recorded", a)
	}
}

func TestOnlyAnOwnerCanViewAs(t *testing.T) {
	w := newWorld(t)
	mgr := w.signedIn(t, "daniel@hotel.com", &w.orgA).member(t, w.orgA, &w.propA1, "manager")
	if r := mgr.do("POST", "/api/view-as", nil, `{"role":"front_desk"}`); r.status != 403 {
		t.Fatalf("manager: %d", r.status)
	}
	owner := w.signedIn(t, "linh@hotel.com", &w.orgA).member(t, w.orgA, nil, "owner")
	if r := owner.do("POST", "/api/view-as", nil, `{"role":"front_desk"}`); r.status != 204 {
		t.Fatalf("owner: %d %s", r.status, r.body)
	}
	var viewAs string
	must(t, db.WithoutOrg(ctx, w.pool, func(tx pgx.Tx) error {
		s, e := session.Lookup(ctx, tx, owner.session, time.Now())
		viewAs = s.ViewAs
		return e
	}))
	if viewAs != "front_desk" {
		t.Fatalf("view as %q", viewAs)
	}
	if r := owner.do("POST", "/api/view-as", nil, `{"role":""}`); r.status != 204 {
		t.Fatalf("clearing: %d", r.status)
	}
	for _, bad := range []string{`{"role":"janitor"}`, `{"role":"owner"}`, `{not json`} {
		if r := owner.do("POST", "/api/view-as", nil, bad); r.status != 400 {
			t.Errorf("%s: %d, want 400", bad, r.status)
		}
	}
}

func TestViewAsNeedsASession(t *testing.T) {
	w := newWorld(t)
	anon := &person{w: w}
	if r := anon.do("POST", "/api/view-as", nil, `{"role":"manager"}`); r.status != 401 {
		t.Fatalf("%d", r.status)
	}
}

func TestPropertyOfAnotherOrganizationIsForbidden(t *testing.T) {
	w := newWorld(t)
	p := w.signedIn(t, "mai@hotel.com", &w.orgA).member(t, w.orgA, &w.propA1, "manager")
	if r := p.can("users.manage", ptr(w.propB)); r.status != 403 {
		t.Fatalf("%d", r.status)
	}
	if r := p.can("users.manage", ptr(uuid.New())); r.status != 403 {
		t.Fatalf("a property that does not exist: %d", r.status)
	}
}

func TestMembershipAtAnotherPropertyOfTheSameOrganizationIsNotEnough(t *testing.T) {
	w := newWorld(t)
	p := w.signedIn(t, "mai@hotel.com", &w.orgA).member(t, w.orgA, &w.propA1, "manager")
	if r := p.can("users.manage", ptr(w.propA2)); r.status != 403 {
		t.Fatalf("%d", r.status)
	}
}

func TestBadPropertyHeaderIs400(t *testing.T) {
	w := newWorld(t)
	p := w.signedIn(t, "mai@hotel.com", &w.orgA).member(t, w.orgA, &w.propA1, "manager")
	req, _ := http.NewRequest("GET", w.srv.URL+"/api/test/users.manage", nil)
	req.AddCookie(&http.Cookie{Name: "sid", Value: p.session})
	req.Header.Set("X-Property-Id", "not-a-uuid")
	resp, err := http.DefaultClient.Do(req)
	must(t, err)
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("%d", resp.StatusCode)
	}
}

func TestNoSessionIs401AndNoOrganizationIs401(t *testing.T) {
	w := newWorld(t)
	if r := (&person{w: w}).can("checkin", ptr(w.propA1)); r.status != 401 {
		t.Fatalf("no cookie: %d", r.status)
	}
	p := w.signedIn(t, "mai@hotel.com", nil).member(t, w.orgA, &w.propA1, "front_desk")
	if r := p.can("checkin", ptr(w.propA1)); r.status != 401 || !strings.Contains(r.body, "organization_required") {
		t.Fatalf("no organization chosen: %d %s", r.status, r.body)
	}
}

func TestRoleChangeAppliesOnTheNextRequest(t *testing.T) {
	w := newWorld(t)
	p := w.signedIn(t, "mai@hotel.com", &w.orgA).member(t, w.orgA, &w.propA1, "front_desk")
	if r := p.can("rates.edit", ptr(w.propA1)); r.status != 403 {
		t.Fatalf("%d", r.status)
	}
	must(t, db.WithOrg(ctx, w.pool, w.orgA, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `update membership set role = 'revenue' where user_id = $1`, p.id)
		return e
	}))
	if r := p.can("rates.edit", ptr(w.propA1)); r.status != 200 {
		t.Fatalf("the new role did not apply to the same session: %d", r.status)
	}
}

func TestDeniedResponseIsAJSONForbiddenError(t *testing.T) {
	w := newWorld(t)
	p := w.signedIn(t, "mai@hotel.com", &w.orgA).member(t, w.orgA, &w.propA1, "housekeeping")
	r := p.can("payments.take", ptr(w.propA1))
	var body struct {
		Error struct{ Code, Message string }
	}
	must(t, json.Unmarshal([]byte(r.body), &body))
	if r.status != 403 || body.Error.Code != "forbidden" || body.Error.Message == "" {
		t.Fatalf("%d %s", r.status, r.body)
	}
}

func TestViewAsRefusesTheWrongMethod(t *testing.T) {
	w := newWorld(t)
	p := w.signedIn(t, "linh@hotel.com", &w.orgA).member(t, w.orgA, nil, "owner")
	if r := p.do("GET", "/api/view-as", nil, ""); r.status != 405 {
		t.Fatalf("%d", r.status)
	}
}

func TestADeactivatedMembershipCannotAct(t *testing.T) {
	w := newWorld(t)
	p := w.signedIn(t, "mai@hotel.com", &w.orgA).member(t, w.orgA, &w.propA1, "manager")
	if r := p.can("users.manage", ptr(w.propA1)); r.status != 200 {
		t.Fatalf("before: %d", r.status)
	}
	must(t, db.WithOrg(ctx, w.pool, w.orgA, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `update membership set status = 'deactivated' where user_id = $1`, p.id)
		return e
	}))
	if r := p.can("users.manage", ptr(w.propA1)); r.status != 403 {
		t.Fatalf("a deactivated manager still acts: %d", r.status)
	}
}
