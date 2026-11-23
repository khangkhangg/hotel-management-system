package extras_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/extras"
	"hotelpms/api/internal/folio"
	"hotelpms/api/internal/rooms"
	"hotelpms/api/internal/testkit"
)

type hotel struct {
	w                  *testkit.World
	prop               uuid.UUID
	owner, desk, other *testkit.Person
	res                string
}

func id(t *testing.T, r testkit.Reply) string {
	t.Helper()
	var b struct{ ID string }
	testkit.Must(t, json.Unmarshal([]byte(r.Body), &b))
	return b.ID
}

func newHotel(t *testing.T) *hotel {
	w := testkit.New(t, rooms.Mount, extras.Mount, booking.Mount, folio.Mount)
	h := &hotel{w: w, prop: w.A1}
	h.owner = w.Member(w.OrgA, nil, "owner")
	h.desk = w.Member(w.OrgA, &w.A1, "front_desk")
	h.other = w.Member(w.OrgA, &w.A1, "housekeeping")
	return h
}

func TestOnlyPeopleWhoSetRatesChangeTheCatalogAndStaffSeeIt(t *testing.T) {
	h := newHotel(t)
	body := `{"name":"Airport pickup","kind":"service","unit":"each","price":2500}`
	h.desk.Do("POST", "/api/extras", &h.prop, body).Want(t, 403)
	x := id(t, h.owner.Do("POST", "/api/extras", &h.prop, body).Want(t, 201))
	h.owner.Do("POST", "/api/extras", &h.prop, `{"name":"","kind":"service","unit":"each","price":2500}`).Want(t, 400)
	h.owner.Do("POST", "/api/extras", &h.prop, `{"name":"Free","kind":"service","unit":"each","price":0}`).Want(t, 400)
	h.owner.Do("POST", "/api/extras", &h.prop, `{"name":"Odd","kind":"spa","unit":"each","price":10}`).Want(t, 400)
	h.owner.Do("POST", "/api/extras", &h.prop, body).Want(t, 409, "name_taken")
	h.desk.Do("GET", "/api/extras", &h.prop, "").Want(t, 200, "Airport pickup")
	h.owner.Do("PATCH", "/api/extras/"+x, &h.prop, `{"active":false}`).Want(t, 200)
	if got := h.desk.Do("GET", "/api/extras", &h.prop, "").Want(t, 200).Body; strings.TrimSpace(got) != "[]" {
		t.Fatalf("a switched-off extra still listed to staff selling: %s", got)
	}
	h.owner.Do("GET", "/api/extras?all=1", &h.prop, "").Want(t, 200, "Airport pickup")
}

// book makes a 2 night stay for 2 adults and returns the reservation id.
func (h *hotel) book(t *testing.T) string {
	rt := id(t, h.owner.Do("POST", "/api/room-types", &h.prop, `{"name":"Deluxe","baseGuests":2,"maxGuests":3}`).Want(t, 201))
	h.owner.Do("POST", "/api/rooms", &h.prop, fmt.Sprintf(`{"roomTypeId":%q,"number":"101","floor":1}`, rt)).Want(t, 201)
	rp := id(t, h.owner.Do("POST", "/api/rate-plans", &h.prop, fmt.Sprintf(`{"roomTypeId":%q,"name":"Flexible"}`, rt)).Want(t, 201))
	h.owner.Do("PUT", "/api/rates", &h.prop, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-11-23","to":"2027-01-01","price":10000}`, rp)).Want(t, 200)
	return id(t, h.desk.Do("POST", "/api/reservations", &h.prop, fmt.Sprintf(
		`{"guest":{"name":"Mai Pham","email":"mai@hotel.com","adults":2,"children":0},"rooms":[{"roomTypeId":%q,"ratePlanId":%q,"from":"2026-12-10","to":"2026-12-12","adults":2,"children":0}]}`, rt, rp)).Want(t, 201))
}

func TestSellingAnExtraPostsOneFolioLineScaledByItsUnit(t *testing.T) {
	h := newHotel(t)
	res := h.book(t)
	each := id(t, h.owner.Do("POST", "/api/extras", &h.prop, `{"name":"Airport pickup","kind":"service","unit":"each","price":2500}`).Want(t, 201))
	night := id(t, h.owner.Do("POST", "/api/extras", &h.prop, `{"name":"Breakfast","kind":"service","unit":"per_person_night","price":800}`).Want(t, 201))
	h.desk.Do("POST", "/api/reservations/"+res+"/extras", &h.prop, fmt.Sprintf(`{"extraId":%q,"quantity":2}`, each)).Want(t, 201)
	h.desk.Do("POST", "/api/reservations/"+res+"/extras", &h.prop, fmt.Sprintf(`{"extraId":%q,"quantity":1}`, night)).Want(t, 201)
	var f struct {
		Lines []struct {
			Description string
			Quantity    int
			UnitPrice   int64
		}
	}
	testkit.Must(t, json.Unmarshal([]byte(h.desk.Do("GET", "/api/reservations/"+res+"/folio", &h.prop, "").Want(t, 200).Body), &f))
	got := map[string]int{}
	for _, l := range f.Lines {
		got[l.Description] = l.Quantity
	}
	if got["Airport pickup"] != 2 || got["Breakfast"] != 4 { // 2 adults x 2 nights
		t.Fatalf("%v", got)
	}
	h.desk.Do("POST", "/api/reservations/"+res+"/extras", &h.prop, `{"extraId":"`+uuid.NewString()+`","quantity":1}`).Want(t, 404)
	h.desk.Do("POST", "/api/reservations/"+res+"/extras", &h.prop, fmt.Sprintf(`{"extraId":%q,"quantity":0}`, each)).Want(t, 400)
	h.owner.Do("PATCH", "/api/extras/"+each, &h.prop, `{"active":false}`).Want(t, 200)
	h.desk.Do("POST", "/api/reservations/"+res+"/extras", &h.prop, fmt.Sprintf(`{"extraId":%q,"quantity":1}`, each)).Want(t, 404)
}

func TestAnExtraCannotBeSoldOnAClosedStayOrInAnAbsurdAmount(t *testing.T) {
	h := newHotel(t)
	res := h.book(t)
	x := id(t, h.owner.Do("POST", "/api/extras", &h.prop, `{"name":"Breakfast","kind":"service","unit":"per_person_night","price":800}`).Want(t, 201))
	h.desk.Do("POST", "/api/reservations/"+res+"/extras", &h.prop, fmt.Sprintf(`{"extraId":%q,"quantity":101}`, x)).Want(t, 400)
	h.desk.Do("POST", "/api/reservations/"+res+"/cancel", &h.prop, "").Want(t, 204)
	h.desk.Do("POST", "/api/reservations/"+res+"/extras", &h.prop, fmt.Sprintf(`{"extraId":%q,"quantity":1}`, x)).Want(t, 400)
}
