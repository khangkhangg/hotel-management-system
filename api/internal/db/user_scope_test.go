package db_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/account"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/db/testdb"
	"hotelpms/api/internal/tenant"
)

// world: org A and org B (each with one property), org C the user has nothing to do with.
// mai works in A and B, lan works in A only.
type world struct {
	pool     *pgxpool.Pool
	a, b, c  uuid.UUID
	mai, lan uuid.UUID
}

func newWorld(t *testing.T) world {
	t.Helper()
	pool := testdb.New(t)
	w := world{pool: pool, a: seedOrg(t, pool, "a"), b: seedOrg(t, pool, "b"), c: seedOrg(t, pool, "c")}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.WithoutOrg(ctx, pool, func(tx pgx.Tx) (e error) { w.mai, e = account.Create(ctx, tx, "mai@hotel.com", "Mai"); return }))
	must(db.WithoutOrg(ctx, pool, func(tx pgx.Tx) (e error) { w.lan, e = account.Create(ctx, tx, "lan@hotel.com", "Lan"); return }))
	for _, m := range []struct{ org, user uuid.UUID }{{w.a, w.mai}, {w.b, w.mai}, {w.a, w.lan}} {
		must(db.WithOrg(ctx, pool, m.org, func(tx pgx.Tx) error { return tenant.AddMembership(ctx, tx, m.org, m.user, nil, "owner") }))
	}
	return w
}

func count(t *testing.T, w world, user uuid.UUID, query string) (n int) {
	t.Helper()
	if err := db.WithUser(ctx, w.pool, user, func(tx pgx.Tx) error { return tx.QueryRow(ctx, query).Scan(&n) }); err != nil {
		t.Fatal(err)
	}
	return
}

func TestUserSeesOwnMembershipsAcrossOrganizations(t *testing.T) {
	w := newWorld(t)
	if n := count(t, w, w.mai, `select count(*) from membership`); n != 2 {
		t.Fatalf("mai sees %d memberships, want her 2", n)
	}
	if n := count(t, w, w.lan, `select count(*) from membership`); n != 1 {
		t.Fatalf("lan sees %d memberships, want her 1", n)
	}
}

func TestUserSeesOnlyOrganizationsTheyBelongTo(t *testing.T) {
	w := newWorld(t)
	if n := count(t, w, w.mai, `select count(*) from organization`); n != 2 {
		t.Fatalf("mai sees %d organizations, want A and B", n)
	}
	if n := count(t, w, w.lan, `select count(*) from organization where id = (select id from organization order by name desc limit 1)`); n > 1 {
		t.Fatal("lan sees more than one organization row for one id")
	}
	if n := count(t, w, w.lan, `select count(*) from organization`); n != 1 {
		t.Fatalf("lan sees %d organizations, want only A", n)
	}
}

func TestUserScopeDoesNotOpenProperties(t *testing.T) {
	w := newWorld(t)
	if n := count(t, w, w.mai, `select count(*) from property`); n != 0 {
		t.Fatalf("a user scope read %d properties; properties need an organization", n)
	}
}

func TestUserScopeCannotWrite(t *testing.T) {
	w := newWorld(t)
	err := db.WithUser(ctx, w.pool, w.mai, func(tx pgx.Tx) error {
		return tenant.AddMembership(ctx, tx, w.c, w.mai, nil, "owner")
	})
	if err == nil {
		t.Fatal("a user scope added a membership to another organization")
	}
	if err := db.WithUser(ctx, w.pool, w.mai, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update membership set role = 'manager'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var role string
	db.WithOrg(ctx, w.pool, w.a, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select role from membership where user_id = $1`, w.mai).Scan(&role)
	})
	if role != "owner" {
		t.Fatalf("a user scope changed a role to %q", role)
	}
}

func TestUserAndOrganizationScopesCombine(t *testing.T) {
	w := newWorld(t)
	var n int
	if err := db.WithOrgUser(ctx, w.pool, w.a, w.mai, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select (select count(*) from membership) * 100 + (select count(*) from property)`).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	// Organization A has two memberships (mai, lan) and one property; the user scope adds mai's membership in B.
	if n != 3*100+1 {
		t.Fatalf("got %d, want 3 memberships (A: mai, lan; B: mai) and 1 property", n)
	}
}

func TestUserScopeDoesNotLeakToNextTransaction(t *testing.T) {
	w := newWorld(t)
	count(t, w, w.mai, `select count(*) from membership`)
	var n int
	db.WithoutOrg(ctx, w.pool, func(tx pgx.Tx) error { return tx.QueryRow(ctx, `select count(*) from membership`).Scan(&n) })
	if n != 0 {
		t.Fatalf("a later transaction saw %d memberships", n)
	}
}

func TestAnInviteIsVisibleOnlyThroughItsOwnToken(t *testing.T) {
	w := newWorld(t)
	token := []byte("hash-of-token-1")
	if err := db.WithOrg(ctx, w.pool, w.a, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `insert into invite (id, organization_id, email, role, token_hash, created_at, expires_at)
			values (gen_random_uuid(), $1, 'new@hotel.com', 'owner', $2, now(), now() + interval '7 days')`, w.a, token)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	seen := func(run func(func(pgx.Tx) error) error) (n int) {
		if err := run(func(tx pgx.Tx) error { return tx.QueryRow(ctx, `select count(*) from invite`).Scan(&n) }); err != nil {
			t.Fatal(err)
		}
		return
	}
	cases := map[string]struct {
		run  func(func(pgx.Tx) error) error
		want int
	}{
		"no scope":           {func(f func(pgx.Tx) error) error { return db.WithoutOrg(ctx, w.pool, f) }, 0},
		"its organization":   {func(f func(pgx.Tx) error) error { return db.WithOrg(ctx, w.pool, w.a, f) }, 1},
		"another one":        {func(f func(pgx.Tx) error) error { return db.WithOrg(ctx, w.pool, w.b, f) }, 0},
		"the right token":    {func(f func(pgx.Tx) error) error { return db.WithInvite(ctx, w.pool, token, f) }, 1},
		"a different token":  {func(f func(pgx.Tx) error) error { return db.WithInvite(ctx, w.pool, []byte("another"), f) }, 0},
		"a signed-in person": {func(f func(pgx.Tx) error) error { return db.WithUser(ctx, w.pool, w.mai, f) }, 0},
	}
	for name, c := range cases {
		if got := seen(c.run); got != c.want {
			t.Errorf("%s sees %d invites, want %d", name, got, c.want)
		}
	}
}
