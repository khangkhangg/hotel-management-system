package signin_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/db"
)

func (a *app) actions(org uuid.UUID) []string {
	a.t.Helper()
	var out []string
	must(a.t, db.WithOrg(ctx, a.pool, org, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select action from audit_log order by seq`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				return err
			}
			out = append(out, s)
		}
		return rows.Err()
	}))
	return out
}

func TestSigningInAndOutIsRecordedInTheOrganization(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	c := a.client()
	c.fullSignIn("mai@hotel.com", goodPassword, false).want(200, `"next":"done"`)
	if got := a.actions(a.site().org); !slices.Equal(got, []string{"signin.succeeded"}) {
		t.Fatalf("after sign-in: %v", got)
	}
	c.post("/api/sign-out", "").want(204)
	if got := a.actions(a.site().org); !slices.Equal(got, []string{"signin.succeeded", "signout"}) {
		t.Fatalf("after sign-out: %v", got)
	}
}

func TestSignOutEverywhereIsRecorded(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	c := a.client()
	c.fullSignIn("mai@hotel.com", goodPassword, false).want(200)
	c.post("/api/sign-out/everywhere", "").want(204)
	if got := a.actions(a.site().org); !slices.Equal(got, []string{"signin.succeeded", "signout"}) {
		t.Fatalf("%v", got)
	}
}

func TestOnlyTheChosenOrganizationSeesTheSignIn(t *testing.T) {
	a := newApp(t)
	first, second := a.newSite("First Hotel"), a.newSite("Second Hotel")
	id := a.seedUser("mai@hotel.com", goodPassword, first, "owner")
	a.addMembership(id, second, "owner")
	c := a.client()
	c.fullSignIn("mai@hotel.com", goodPassword, false).want(200, "choose_organization")
	if len(a.actions(first.org))+len(a.actions(second.org)) != 0 {
		t.Fatal("a sign-in was recorded before an organization was chosen")
	}
	c.post("/api/sign-in/organization", fmt.Sprintf(`{"organizationId":%q}`, second.org)).want(204)
	if got := a.actions(second.org); !slices.Equal(got, []string{"signin.succeeded"}) {
		t.Fatalf("chosen organization: %v", got)
	}
	if got := a.actions(first.org); len(got) != 0 {
		t.Fatalf("the other organization saw it: %v", got)
	}
}

func TestAWrongPasswordIsRecordedOnlyForARealAccount(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	a.client().signIn("mai@hotel.com", "not the password").want(401)
	a.client().signIn("nobody@hotel.com", "not the password").want(401)
	if got := a.actions(a.site().org); !slices.Equal(got, []string{"signin.failed"}) {
		t.Fatalf("%v", got)
	}
}

func TestAWrongEmailCodeIsRecorded(t *testing.T) {
	a := newApp(t)
	a.seedUser("mai@hotel.com", goodPassword, a.site(), "front_desk")
	c := a.client()
	c.signIn("mai@hotel.com", goodPassword).want(200, `"next":"code"`)
	c.enterCode("000000", false).want(400, "code_wrong")
	if got := a.actions(a.site().org); !slices.Equal(got, []string{"signin.failed"}) {
		t.Fatalf("%v", got)
	}
}

func TestOrganizationsListsTheChoicesForASessionWithNoneChosen(t *testing.T) {
	a := newApp(t)
	first, second := a.newSite("First Hotel"), a.newSite("Second Hotel")
	id := a.seedUser("mai@hotel.com", goodPassword, first, "owner")
	a.addMembership(id, second, "owner")
	c := a.client()
	c.get("/api/organizations").want(401)
	c.fullSignIn("mai@hotel.com", goodPassword, false).want(200, "choose_organization")
	c.get("/api/organizations").want(200, "First Hotel", "Second Hotel", first.org.String())
}
