package chain_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/chain"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/rooms"
	"hotelpms/api/internal/testkit"
)

func idOf(t *testing.T, r testkit.Reply) string {
	t.Helper()
	var b struct{ ID string }
	testkit.Must(t, json.Unmarshal([]byte(r.Body), &b))
	return b.ID
}

const policy = `"freeCancelUntilDays":2,"freeCancelUntilTime":"18:00","lateCancelPenalty":"first_night","mealPlan":"breakfast"`

func TestAChainSetsARatePlanOnceAndEachPropertyLinksToIt(t *testing.T) {
	w := testkit.New(t, rooms.Mount, chain.Mount)
	owner := w.Member(w.OrgA, nil, "owner")
	manager := w.Member(w.OrgA, &w.A1, "manager")
	otherOwner := w.Member(w.OrgB, nil, "owner")
	rt := idOf(t, manager.Do("POST", "/api/room-types", &w.A1, `{"name":"Deluxe","baseGuests":2,"maxGuests":3}`).Want(t, 201))

	manager.Do("POST", "/api/rate-plan-templates", &w.A1, `{"name":"Flexible",`+policy+`}`).Want(t, 403) // one hotel's manager cannot set the chain's plans
	owner.Do("POST", "/api/rate-plan-templates", nil, `{"name":"","lateCancelPenalty":"none","mealPlan":"none"}`).Want(t, 400)
	tpl := idOf(t, owner.Do("POST", "/api/rate-plan-templates", &w.A1, `{"name":"Flexible",`+policy+`}`).Want(t, 201))
	owner.Do("POST", "/api/rate-plan-templates", nil, `{"name":"Flexible","lateCancelPenalty":"none","mealPlan":"none"}`).Want(t, 409, "name_taken")
	manager.Do("GET", "/api/rate-plan-templates", &w.A1, "").Want(t, 200, "Flexible") // a property sees what it can link to

	plan := idOf(t, manager.Do("POST", "/api/rate-plan-templates/"+tpl+"/link", &w.A1, fmt.Sprintf(`{"roomTypeId":%q}`, rt)).Want(t, 201))
	manager.Do("POST", "/api/rate-plan-templates/"+tpl+"/link", &w.A1, fmt.Sprintf(`{"roomTypeId":%q}`, rt)).Want(t, 409, "name_taken")
	otherOwner.Do("POST", "/api/rate-plan-templates/"+tpl+"/link", &w.B1, fmt.Sprintf(`{"roomTypeId":%q}`, rt)).Want(t, 404) // another organization's template is not there

	read := func() (name, penalty, meal string, days int, linked bool) {
		testkit.Must(t, db.WithOrg(testkit.Ctx, w.Pool, w.OrgA, func(tx pgx.Tx) error {
			return tx.QueryRow(testkit.Ctx, `select name, late_cancel_penalty, meal_plan, free_cancel_until_days, template_id is not null from rate_plan where id = $1`, plan).Scan(&name, &penalty, &meal, &days, &linked)
		}))
		return
	}
	if n, p, m, d, l := read(); n != "Flexible" || p != "first_night" || m != "breakfast" || d != 2 || !l {
		t.Fatalf("%s %s %s %d %v", n, p, m, d, l)
	}
	manager.Do("PUT", "/api/rate-plan-templates/"+tpl, &w.A1, `{"name":"Flexible",`+policy+`}`).Want(t, 403)
	owner.Do("PUT", "/api/rate-plan-templates/"+tpl, &w.A1, `{"name":"Flexible plus","freeCancelUntilDays":5,"lateCancelPenalty":"full","mealPlan":"breakfast"}`).Want(t, 200)
	if n, p, _, d, _ := read(); n != "Flexible plus" || p != "full" || d != 5 {
		t.Fatalf("a change to the chain's plan did not reach the property: %s %s %d", n, p, d)
	}
}
