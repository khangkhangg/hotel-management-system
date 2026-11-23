package group_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/folio"
	"hotelpms/api/internal/group"
	"hotelpms/api/internal/rooms"
	"hotelpms/api/internal/testkit"
)

func idOf(t *testing.T, r testkit.Reply) string {
	t.Helper()
	var b struct{ ID string }
	testkit.Must(t, json.Unmarshal([]byte(r.Body), &b))
	return b.ID
}

type row struct {
	ID                                 uuid.UUID
	Name, Currency                     string
	Rooms, Occupied, Arriving, Leaving int
	Paid30                             int64
}

type view struct {
	Properties []row
	Totals     []row
}

func TestTheOwnerSeesEveryPropertyOfTheirOrganizationAndTheSums(t *testing.T) {
	w := testkit.New(t, rooms.Mount, booking.Mount, folio.Mount, group.Mount)
	owner := w.Member(w.OrgA, nil, "owner")
	manager := w.Member(w.OrgA, &w.A1, "manager")
	rt := idOf(t, owner.Do("POST", "/api/room-types", &w.A1, `{"name":"Deluxe","baseGuests":2,"maxGuests":3}`).Want(t, 201))
	owner.Do("POST", "/api/rooms", &w.A1, fmt.Sprintf(`{"roomTypeId":%q,"number":"101","floor":1}`, rt)).Want(t, 201)
	rp := idOf(t, owner.Do("POST", "/api/rate-plans", &w.A1, fmt.Sprintf(`{"roomTypeId":%q,"name":"Flexible"}`, rt)).Want(t, 201))
	owner.Do("PUT", "/api/rates", &w.A1, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-11-23","to":"2027-01-01","price":10000}`, rp)).Want(t, 200)
	res := idOf(t, owner.Do("POST", "/api/reservations", &w.A1, fmt.Sprintf(
		`{"guest":{"name":"Mai Pham","email":"mai@hotel.com"},"rooms":[{"roomTypeId":%q,"ratePlanId":%q,"from":"2026-11-23","to":"2026-11-25","adults":2,"children":0}]}`, rt, rp)).Want(t, 201))
	var f struct{ FolioID string }
	testkit.Must(t, json.Unmarshal([]byte(owner.Do("GET", "/api/reservations/"+res+"/folio", &w.A1, "").Want(t, 200).Body), &f))
	owner.Do("POST", "/api/folios/"+f.FolioID+"/payments", &w.A1, `{"method":"cash","amount":5000}`).Want(t, 201)

	manager.Do("GET", "/api/group", nil, "").Want(t, 403)
	manager.Do("GET", "/api/group", &w.A1, "").Want(t, 403) // the web app names the property, and that must not open the group view
	owner.Do("GET", "/api/group", &w.A1, "").Want(t, 200)
	var v view
	testkit.Must(t, json.Unmarshal([]byte(owner.Do("GET", "/api/group", nil, "").Want(t, 200).Body), &v))
	if len(v.Properties) != 2 {
		t.Fatalf("%d properties: the other organization's hotel must not show", len(v.Properties))
	}
	by := map[uuid.UUID]row{}
	for _, p := range v.Properties {
		by[p.ID] = p
	}
	if a1 := by[w.A1]; a1.Rooms != 1 || a1.Arriving != 1 || a1.Paid30 != 5000 {
		t.Fatalf("A1 %+v", a1)
	}
	if a2 := by[w.A2]; a2.Rooms != 0 || a2.Arriving != 0 || a2.Paid30 != 0 {
		t.Fatalf("A2 %+v", a2)
	}
	if len(v.Totals) != 1 || v.Totals[0].Rooms != 1 || v.Totals[0].Arriving != 1 || v.Totals[0].Paid30 != 5000 {
		t.Fatalf("totals %+v", v.Totals)
	}
}
