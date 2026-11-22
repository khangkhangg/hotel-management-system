package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/db"
	"hotelpms/api/internal/db/testdb"
	"hotelpms/api/internal/tenant"
)

var ctx = context.Background()

func seedOrg(t *testing.T, pool *pgxpool.Pool, name string) uuid.UUID {
	t.Helper()
	var org uuid.UUID
	if err := db.WithoutOrg(ctx, pool, func(tx pgx.Tx) (err error) {
		org, err = tenant.CreateOrganization(ctx, tx, name)
		return
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.WithOrg(ctx, pool, org, func(tx pgx.Tx) error {
		_, err := tenant.CreateProperty(ctx, tx, org, tenant.PropertyInput{Name: name + " hotel", Slug: "hotel-" + name, TimeZone: "Asia/Ho_Chi_Minh", Currency: "USD", DefaultLocale: "en"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return org
}

func countProperties(t *testing.T, pool *pgxpool.Pool, org uuid.UUID) int {
	t.Helper()
	var n int
	if err := db.WithOrg(ctx, pool, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select count(*) from property`).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRLSBlocksOtherOrganization(t *testing.T) {
	pool := testdb.New(t)
	a := seedOrg(t, pool, "a")
	b := seedOrg(t, pool, "b")
	if n := countProperties(t, pool, a); n != 1 {
		t.Fatalf("org A sees %d properties, want only its own 1", n)
	}
	if n := countProperties(t, pool, b); n != 1 {
		t.Fatalf("org B sees %d properties, want only its own 1", n)
	}
}

func TestOrganizationTableIsAlsoIsolated(t *testing.T) {
	pool := testdb.New(t)
	a := seedOrg(t, pool, "a")
	seedOrg(t, pool, "b")
	var n int
	if err := db.WithOrg(ctx, pool, a, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select count(*) from organization`).Scan(&n)
	}); err != nil || n != 1 {
		t.Fatalf("org A sees %d organizations (%v), want 1", n, err)
	}
}

func TestNoOrganizationContextSeesNothing(t *testing.T) {
	pool := testdb.New(t)
	seedOrg(t, pool, "a")
	var n int
	if err := db.WithoutOrg(ctx, pool, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select (select count(*) from property) + (select count(*) from organization)`).Scan(&n)
	}); err != nil || n != 0 {
		t.Fatalf("no context saw %d rows (%v), want 0", n, err)
	}
}

func TestInsertIntoAnotherOrganizationIsRejected(t *testing.T) {
	pool := testdb.New(t)
	a := seedOrg(t, pool, "a")
	b := seedOrg(t, pool, "b")
	err := db.WithOrg(ctx, pool, a, func(tx pgx.Tx) error {
		_, err := tenant.CreateProperty(ctx, tx, b, tenant.PropertyInput{Name: "sneaky", Slug: "sneaky", TimeZone: "UTC", Currency: "USD", DefaultLocale: "en"})
		return err
	})
	if err == nil {
		t.Fatal("org A wrote a property into org B")
	}
}

func TestWithOrgRollsBackOnError(t *testing.T) {
	pool := testdb.New(t)
	a := seedOrg(t, pool, "a")
	boom := errors.New("boom")
	err := db.WithOrg(ctx, pool, a, func(tx pgx.Tx) error {
		if _, err := tenant.CreateProperty(ctx, tx, a, tenant.PropertyInput{Name: "second", Slug: "second", TimeZone: "UTC", Currency: "USD", DefaultLocale: "en"}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	if n := countProperties(t, pool, a); n != 1 {
		t.Fatalf("%d properties after rollback, want 1", n)
	}
}

func TestOrganizationSettingDoesNotLeakToNextTransaction(t *testing.T) {
	pool := testdb.New(t)
	a := seedOrg(t, pool, "a")
	countProperties(t, pool, a)
	var n int
	if err := db.WithoutOrg(ctx, pool, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select count(*) from property`).Scan(&n)
	}); err != nil || n != 0 {
		t.Fatalf("a later transaction saw %d rows (%v): the organization setting leaked", n, err)
	}
}

func TestPropertySlugIsUnique(t *testing.T) {
	pool := testdb.New(t)
	a := seedOrg(t, pool, "a")
	err := db.WithOrg(ctx, pool, a, func(tx pgx.Tx) error {
		_, err := tenant.CreateProperty(ctx, tx, a, tenant.PropertyInput{Name: "dup", Slug: "hotel-a", TimeZone: "UTC", Currency: "USD", DefaultLocale: "en"})
		return err
	})
	if err == nil {
		t.Fatal("duplicate slug accepted")
	}
}
