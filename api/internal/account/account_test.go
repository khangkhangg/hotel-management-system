package account_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/account"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/db/testdb"
	"hotelpms/api/internal/tenant"
)

var ctx = context.Background()

func TestNormalizeEmail(t *testing.T) {
	for _, in := range []string{" Mai@Hotel.com ", "MAI@HOTEL.COM", "mai@hotel.com", "\tmai@hotel.com\n"} {
		if got := account.NormalizeEmail(in); got != "mai@hotel.com" {
			t.Errorf("%q -> %q", in, got)
		}
	}
}

func TestNormalizeEmailKeepsDotsAndPlusTags(t *testing.T) {
	if got := account.NormalizeEmail("Mai.Pham+desk@Hotel.com"); got != "mai.pham+desk@hotel.com" {
		t.Fatal(got)
	}
}

func create(pool *pgxpool.Pool, email string) (id uuid.UUID, err error) {
	err = db.WithoutOrg(ctx, pool, func(tx pgx.Tx) error {
		id, err = account.Create(ctx, tx, email, "Mai Pham")
		return err
	})
	return
}

func TestCreateRejectsSameEmailInAnotherForm(t *testing.T) {
	pool := testdb.New(t)
	if _, err := create(pool, "mai@hotel.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := create(pool, " MAI@Hotel.com"); err == nil {
		t.Fatal("duplicate accepted")
	}
}

func TestDatabaseRejectsAnEmailThatIsNotNormalised(t *testing.T) {
	pool := testdb.New(t)
	// Control: the same statement with a normalised address works, so the rejection below is the check constraint.
	if _, err := pool.Exec(ctx, `insert into app_user (id, email, name) values (gen_random_uuid(), 'ok@hotel.com', 'x')`); err != nil {
		t.Fatalf("control insert failed: %v", err)
	}
	_, err := pool.Exec(ctx, `insert into app_user (id, email, name) values (gen_random_uuid(), ' Mai@Hotel.com', 'x')`)
	if err == nil {
		t.Fatal("the table accepted an email that is not trimmed and lower case")
	}
}

func TestByEmailFindsAnyForm(t *testing.T) {
	pool := testdb.New(t)
	id, _ := create(pool, "mai@hotel.com")
	var got account.User
	err := db.WithoutOrg(ctx, pool, func(tx pgx.Tx) (e error) {
		got, e = account.ByEmail(ctx, tx, "  MAI@hotel.COM ")
		return
	})
	if err != nil || got.ID != id || got.Email != "mai@hotel.com" || got.Name != "Mai Pham" || got.Status != "active" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestByEmailUnknownIsErrNotFound(t *testing.T) {
	pool := testdb.New(t)
	err := db.WithoutOrg(ctx, pool, func(tx pgx.Tx) error {
		_, e := account.ByEmail(ctx, tx, "nobody@hotel.com")
		return e
	})
	if !errors.Is(err, account.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestAppRoleCannotDeleteUsers(t *testing.T) {
	pool := testdb.New(t)
	create(pool, "mai@hotel.com")
	if _, err := pool.Exec(ctx, `delete from app_user`); err == nil {
		t.Fatal("the application role deleted a user; users are only deactivated")
	}
}

func addMember(pool *pgxpool.Pool, org, user uuid.UUID, property *uuid.UUID, role string) error {
	return db.WithOrg(ctx, pool, org, func(tx pgx.Tx) error {
		return tenant.AddMembership(ctx, tx, org, user, property, role)
	})
}

func seed(t *testing.T) (pool *pgxpool.Pool, org, prop, user uuid.UUID) {
	t.Helper()
	pool = testdb.New(t)
	if err := db.WithoutOrg(ctx, pool, func(tx pgx.Tx) (e error) { org, e = tenant.CreateOrganization(ctx, tx, "A"); return }); err != nil {
		t.Fatal(err)
	}
	if err := db.WithOrg(ctx, pool, org, func(tx pgx.Tx) (e error) {
		prop, e = tenant.CreateProperty(ctx, tx, org, tenant.PropertyInput{Name: "A", Slug: "a", TimeZone: "UTC", Currency: "USD", DefaultLocale: "en"})
		return
	}); err != nil {
		t.Fatal(err)
	}
	var err error
	if user, err = create(pool, "mai@hotel.com"); err != nil {
		t.Fatal(err)
	}
	return
}

func TestMembershipRoleMustBeOneOfTheFive(t *testing.T) {
	pool, org, prop, user := seed(t)
	if err := addMember(pool, org, user, &prop, "janitor"); err == nil {
		t.Fatal("unknown role accepted")
	}
	for _, r := range []string{"owner", "manager", "front_desk", "revenue", "housekeeping"} {
		u, _ := create(pool, r+"@hotel.com")
		if err := addMember(pool, org, u, &prop, r); err != nil {
			t.Errorf("%s rejected: %v", r, err)
		}
	}
}

func TestSameUserAndPropertyOnlyOnce(t *testing.T) {
	pool, org, prop, user := seed(t)
	if err := addMember(pool, org, user, &prop, "front_desk"); err != nil {
		t.Fatal(err)
	}
	if err := addMember(pool, org, user, &prop, "manager"); err == nil {
		t.Fatal("second membership for the same property accepted")
	}
}

func TestOwnerMembershipWithoutPropertyIsUniquePerOrganization(t *testing.T) {
	pool, org, _, user := seed(t)
	if err := addMember(pool, org, user, nil, "owner"); err != nil {
		t.Fatal(err)
	}
	if err := addMember(pool, org, user, nil, "owner"); err == nil {
		t.Fatal("a second organization-wide membership was accepted")
	}
}
