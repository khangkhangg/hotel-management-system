package lostfound_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"hotelpms/api/internal/lostfound"
	"hotelpms/api/internal/testkit"
)

type hotel struct {
	w                          *testkit.World
	prop                       uuid.UUID
	keeper, desk, mgr, revenue *testkit.Person
}

func newHotel(t *testing.T) *hotel {
	w := testkit.New(t, lostfound.Mount)
	h := &hotel{w: w, prop: w.A1}
	h.keeper = w.Member(w.OrgA, &w.A1, "housekeeping")
	h.desk = w.Member(w.OrgA, &w.A1, "front_desk")
	h.mgr = w.Member(w.OrgA, &w.A1, "manager")
	h.revenue = w.Member(w.OrgA, &w.A1, "revenue")
	return h
}

func idOf(t *testing.T, r testkit.Reply) string {
	t.Helper()
	var b struct{ ID string }
	testkit.Must(t, json.Unmarshal([]byte(r.Body), &b))
	return b.ID
}

func TestHousekeepingLogsWhatItFindsAndTheFrontDeskCanSeeIt(t *testing.T) {
	h := newHotel(t)
	h.revenue.Do("GET", "/api/lost-items", &h.prop, "").Want(t, 403)
	h.revenue.Do("POST", "/api/lost-items", &h.prop, `{"description":"Blue umbrella","foundOn":"2026-11-22"}`).Want(t, 403)
	h.desk.Do("POST", "/api/lost-items", &h.prop, `{"description":"Blue umbrella","foundOn":"2026-11-22"}`).Want(t, 403) // the front desk looks and returns, housekeeping logs
	h.keeper.Do("POST", "/api/lost-items", &h.prop, `{"description":"Blue umbrella","location":"Room 101","foundOn":"2026-11-22"}`).Want(t, 201, `"status":"stored"`)
	h.desk.Do("GET", "/api/lost-items", &h.prop, "").Want(t, 200, "Blue umbrella", "Room 101", `"status":"stored"`)
}

func TestAnItemIsRefusedWhenItIsNotSensibleAndKeepsOnlyKnownStatuses(t *testing.T) {
	h := newHotel(t)
	for _, bad := range []string{`{"description":"","foundOn":"2026-11-22"}`, `{"description":"x","foundOn":"yesterday"}`, `{"description":"` + strings.Repeat("a", 301) + `","foundOn":"2026-11-22"}`, `{"description":"x","foundOn":"2026-11-22","status":"sold"}`, `{}`} {
		h.keeper.Do("POST", "/api/lost-items", &h.prop, bad).Want(t, 400)
	}
}

func TestReturningAnItemRecordsWhoGotItAndAThrownAwayItemLeavesTheShelf(t *testing.T) {
	h := newHotel(t)
	id := idOf(t, h.keeper.Do("POST", "/api/lost-items", &h.prop, `{"description":"Black wallet","foundOn":"2026-11-22"}`).Want(t, 201))
	h.keeper.Do("PUT", "/api/lost-items/"+id, &h.prop, `{"description":"Black wallet","foundOn":"2026-11-22","status":"returned","ownerName":"Kim Park","ownerContact":"kim@example.com"}`).Want(t, 200, `"status":"returned"`, "Kim Park")
	h.desk.Do("GET", "/api/lost-items", &h.prop, "").Want(t, 200, `"ownerName":"Kim Park"`)
	h.desk.Do("GET", "/api/lost-items?status=stored", &h.prop, "").Want(t, 200, "[]")
	// Once an item is returned or discarded it cannot go back to the shelf by accident.
	h.keeper.Do("PUT", "/api/lost-items/"+id, &h.prop, `{"description":"Black wallet","foundOn":"2026-11-22","status":"stored"}`).Want(t, 409)
	h.keeper.Do("PUT", "/api/lost-items/"+uuid.NewString(), &h.prop, `{"description":"x","foundOn":"2026-11-22","status":"stored"}`).Want(t, 404)
	h.desk.Do("PUT", "/api/lost-items/"+id, &h.prop, `{"description":"Black wallet","foundOn":"2026-11-22","status":"returned"}`).Want(t, 403)
}

func TestAnotherPropertyNeverSeesOrChangesThisPropertysItems(t *testing.T) {
	h := newHotel(t)
	id := idOf(t, h.keeper.Do("POST", "/api/lost-items", &h.prop, `{"description":"Scarf","foundOn":"2026-11-22"}`).Want(t, 201))
	other := h.w.A2
	owner := h.w.Member(h.w.OrgA, nil, "owner")
	owner.Do("GET", "/api/lost-items", &other, "").Want(t, 200, "[]")
	owner.Do("PUT", "/api/lost-items/"+id, &other, `{"description":"Scarf","foundOn":"2026-11-22","status":"returned"}`).Want(t, 404)
}
