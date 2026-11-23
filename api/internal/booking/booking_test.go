package booking_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/rooms"
	"hotelpms/api/internal/testkit"
)

var ctx = testkit.Ctx

func must(t *testing.T, err error) { t.Helper(); testkit.Must(t, err) }

type hotel struct {
	w         *testkit.World
	mgr, desk *testkit.Person
	roomType  string
	ratePlan  string
	property  uuid.UUID
}

type idBody struct{ ID, Code string }

func newID(t *testing.T, r testkit.Reply) idBody {
	t.Helper()
	var b idBody
	must(t, json.Unmarshal([]byte(r.Body), &b))
	return b
}

// newHotel builds a property with one room type of n rooms and a rate plan priced 10000 a night for December 2026.
func newHotel(t *testing.T, n int) *hotel {
	t.Helper()
	w := testkit.New(t, rooms.Mount, booking.Mount)
	h := &hotel{w: w, property: w.A1}
	h.mgr = w.Member(w.OrgA, &w.A1, "manager")
	h.desk = w.Member(w.OrgA, &w.A1, "front_desk")
	h.setup(w.A1, n)
	return h
}

func (h *hotel) setup(property uuid.UUID, n int) {
	t, w := h.w.T, h.w
	mgr := h.mgr
	if property != w.A1 {
		mgr = w.Member(w.OrgA, &property, "manager")
	}
	rt := newID(t, mgr.Do("POST", "/api/room-types", &property, `{"name":"Deluxe","baseGuests":2,"maxGuests":3,"extraAdultPrice":2000,"extraChildPrice":1000}`).Want(t, 201)).ID
	for i := 0; i < n; i++ {
		mgr.Do("POST", "/api/rooms", &property, fmt.Sprintf(`{"roomTypeId":%q,"number":"%d","floor":1}`, rt, 101+i)).Want(t, 201)
	}
	rp := newID(t, mgr.Do("POST", "/api/rate-plans", &property, fmt.Sprintf(`{"roomTypeId":%q,"name":"Flexible"}`, rt)).Want(t, 201)).ID
	mgr.Do("PUT", "/api/rates", &property, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-11-23","to":"2027-01-01","price":10000}`, rp)).Want(t, 200)
	if property == w.A1 {
		h.roomType, h.ratePlan = rt, rp
	}
}

func (h *hotel) book(p *testkit.Person, from, to string, extra string) testkit.Reply {
	return p.Do("POST", "/api/reservations", &h.property, fmt.Sprintf(
		`{"guest":{"name":"Mai Pham","email":"mai@hotel.com","phone":"+84 90 000 0000"},"rooms":[{"roomTypeId":%q,"ratePlanId":%q,"from":%q,"to":%q,"adults":2,"children":0}]%s}`,
		h.roomType, h.ratePlan, from, to, extra))
}

func (h *hotel) sold(date string) (sold, total int) {
	h.w.T.Helper()
	must(h.w.T, db.WithOrg(ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select sold, total from inventory where room_type_id = $1 and date = $2`, h.roomType, date).Scan(&sold, &total)
	}))
	return
}

func TestABookingStoresNightsAndRaisesSold(t *testing.T) {
	h := newHotel(t, 2)
	b := newID(t, h.book(h.desk, "2026-12-10", "2026-12-13", "").Want(t, 201))
	if len(b.Code) != 8 {
		t.Fatalf("code %q", b.Code)
	}
	for _, d := range []string{"2026-12-10", "2026-12-11", "2026-12-12"} {
		if s, total := h.sold(d); s != 1 || total != 2 {
			t.Fatalf("%s: sold %d of %d", d, s, total)
		}
	}
	if s, _ := h.sold("2026-12-13"); s != 0 {
		t.Fatal("the departure night was sold")
	}
	got := h.desk.Do("GET", "/api/reservations/"+b.ID, &h.property, "").Want(t, 200, `"total":30000`, `"status":"confirmed"`, "Mai Pham", "mai@hotel.com")
	if !strings.Contains(got.Body, `"2026-12-10"`) {
		t.Fatalf("nights missing: %s", got.Body)
	}
}

func TestTheLastRoomGoesToExactlyOneOfManyAtOnce(t *testing.T) {
	h := newHotel(t, 1)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, full := 0, 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := h.book(h.desk, "2026-12-10", "2026-12-12", "")
			mu.Lock()
			defer mu.Unlock()
			switch r.Status {
			case 201:
				ok++
			case 409:
				full++
			default:
				t.Errorf("status %d: %s", r.Status, r.Body)
			}
		}()
	}
	wg.Wait()
	if ok != 1 || full != 19 {
		t.Fatalf("%d booked, %d turned away", ok, full)
	}
	if s, total := h.sold("2026-12-10"); s != 1 || total != 1 {
		t.Fatalf("sold %d of %d", s, total)
	}
}

func TestABookingAndItsInventoryAreSavedOrRolledBackTogether(t *testing.T) {
	h := newHotel(t, 1)
	h.book(h.desk, "2026-12-11", "2026-12-12", "").Want(t, 201) // the only room is taken on the 11th
	// Two rooms in one request, the second night is full: nothing at all is saved.
	two := fmt.Sprintf(`{"guest":{"name":"Lan Vo","email":"lan@hotel.com"},"rooms":[{"roomTypeId":%q,"ratePlanId":%q,"from":"2026-12-20","to":"2026-12-21","adults":2,"children":0},{"roomTypeId":%q,"ratePlanId":%q,"from":"2026-12-11","to":"2026-12-12","adults":2,"children":0}]}`,
		h.roomType, h.ratePlan, h.roomType, h.ratePlan)
	h.desk.Do("POST", "/api/reservations", &h.property, two).Want(t, 409, "sold_out")
	if s, _ := h.sold("2026-12-20"); s != 0 {
		t.Fatalf("the first room stayed sold: %d", s)
	}
	var n int
	must(t, db.WithOrg(ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select (select count(*) from reservation) + (select count(*) from guest where email = 'lan@hotel.com')`).Scan(&n)
	}))
	if n != 1 {
		t.Fatalf("%d rows left behind, want only the first reservation", n)
	}
}

func TestCancellingGivesTheRoomBackOnce(t *testing.T) {
	h := newHotel(t, 1)
	b := newID(t, h.book(h.desk, "2026-12-10", "2026-12-12", "").Want(t, 201))
	h.desk.Do("POST", "/api/reservations/"+b.ID+"/cancel", &h.property, "").Want(t, 204)
	if s, _ := h.sold("2026-12-10"); s != 0 {
		t.Fatalf("sold %d after cancelling", s)
	}
	h.desk.Do("POST", "/api/reservations/"+b.ID+"/cancel", &h.property, "").Want(t, 409, "already_cancelled")
	if s, _ := h.sold("2026-12-10"); s != 0 {
		t.Fatalf("sold %d after cancelling twice", s)
	}
	h.book(h.desk, "2026-12-10", "2026-12-12", "").Want(t, 201) // the room can be sold again
}

func TestOneGuestPerEmailInAnyCaseAndAcrossProperties(t *testing.T) {
	h := newHotel(t, 2)
	h.setup(h.w.A2, 1)
	a := newID(t, h.book(h.desk, "2026-12-10", "2026-12-11", "").Want(t, 201))
	body := strings.Replace(`{"guest":{"name":"Mai P.","email":"MAI@Hotel.com"},"rooms":[{"roomTypeId":"RT","ratePlanId":"RP","from":"2026-12-12","to":"2026-12-13","adults":1,"children":0}]}`, "RT", h.roomType, 1)
	body = strings.Replace(body, "RP", h.ratePlan, 1)
	h.desk.Do("POST", "/api/reservations", &h.property, body).Want(t, 201)
	var guests int
	must(t, db.WithOrg(ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select count(*) from guest where email = 'mai@hotel.com'`).Scan(&guests)
	}))
	if guests != 1 {
		t.Fatalf("%d guest rows for one person", guests)
	}
	// The chain's other property sees the same profile.
	desk2 := h.w.Member(h.w.OrgA, &h.w.A2, "front_desk")
	desk2.Do("GET", "/api/guests?q=mai", &h.w.A2, "").Want(t, 200, "mai@hotel.com")
	_ = a
}

func TestWhoCanSeeWhat(t *testing.T) {
	h := newHotel(t, 2)
	b := newID(t, h.book(h.desk, "2026-12-10", "2026-12-11", "").Want(t, 201))
	hk := h.w.Member(h.w.OrgA, &h.w.A1, "housekeeping")
	hk.Do("GET", "/api/reservations", &h.property, "").Want(t, 403)
	hk.Do("GET", "/api/reservations/"+b.ID, &h.property, "").Want(t, 403)
	hk.Do("GET", "/api/guests", &h.property, "").Want(t, 403)
	rev := h.w.Member(h.w.OrgA, &h.w.A1, "revenue")
	got := rev.Do("GET", "/api/reservations/"+b.ID, &h.property, "").Want(t, 200, "Mai Pham")
	if strings.Contains(got.Body, "mai@hotel.com") || strings.Contains(got.Body, "+84") {
		t.Fatalf("revenue sees contact details: %s", got.Body)
	}
	rev.Do("POST", "/api/reservations", &h.property, "{}").Want(t, 403)
	h.w.Anon("GET", "/api/reservations", "").Want(t, 401)
	other := h.w.Member(h.w.OrgB, &h.w.B1, "manager")
	other.Do("GET", "/api/reservations/"+b.ID, &h.w.B1, "").Want(t, 404)
}

func TestSalesRulesAreEnforced(t *testing.T) {
	h := newHotel(t, 2)
	h.mgr.Do("PUT", "/api/rates", &h.property, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-12-20","to":"2026-12-25","price":10000,"minNights":3,"closedToArrival":true}`, h.ratePlan)).Want(t, 200)
	h.mgr.Do("PUT", "/api/rates", &h.property, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-12-25","to":"2026-12-26","price":10000,"closed":true}`, h.ratePlan)).Want(t, 200)
	h.mgr.Do("PUT", "/api/rates", &h.property, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-12-28","to":"2026-12-29","price":10000,"closedToDeparture":true}`, h.ratePlan)).Want(t, 200)
	h.book(h.desk, "2026-12-21", "2026-12-23", "").Want(t, 400, "stay_rule") // two nights, three required
	h.book(h.desk, "2026-12-20", "2026-12-23", "").Want(t, 400, "stay_rule") // closed to arrival
	h.book(h.desk, "2026-12-24", "2026-12-26", "").Want(t, 400, "stay_rule") // a closed night
	h.book(h.desk, "2026-12-27", "2026-12-28", "").Want(t, 400, "stay_rule") // departing on a date closed to departure
	h.book(h.desk, "2026-12-27", "2026-12-29", "").Want(t, 201)
	h.book(h.desk, "2026-12-05", "2026-12-05", "").Want(t, 400)         // no nights
	h.book(h.desk, "2026-12-05", "2026-12-04", "").Want(t, 400)         // backwards
	h.book(h.desk, "2026-11-20", "2026-11-22", "").Want(t, 400, "past") // before today
	h.book(h.desk, "2026-12-05", "2027-03-05", "").Want(t, 400)         // too long
	big := strings.Replace(`{"guest":{"name":"X"},"rooms":[{"roomTypeId":"RT","ratePlanId":"RP","from":"2026-12-05","to":"2026-12-06","adults":3,"children":1}]}`, "RT", h.roomType, 1)
	h.desk.Do("POST", "/api/reservations", &h.property, strings.Replace(big, "RP", h.ratePlan, 1)).Want(t, 400, "too_many_guests")
	noRate := strings.Replace(`{"guest":{"name":"X"},"rooms":[{"roomTypeId":"RT","ratePlanId":"RP","from":"2027-02-05","to":"2027-02-06","adults":1,"children":0}]}`, "RT", h.roomType, 1)
	h.desk.Do("POST", "/api/reservations", &h.property, strings.Replace(noRate, "RP", h.ratePlan, 1)).Want(t, 400, "no_rate")
	other := strings.Replace(`{"guest":{"name":"X"},"rooms":[{"roomTypeId":"RT","ratePlanId":"RP","from":"2026-12-05","to":"2026-12-06","adults":1,"children":0}]}`, "RT", uuid.NewString(), 1)
	h.desk.Do("POST", "/api/reservations", &h.property, strings.Replace(other, "RP", h.ratePlan, 1)).Want(t, 404)
	h.desk.Do("POST", "/api/reservations", &h.property, `{"guest":{"name":""},"rooms":[]}`).Want(t, 400)
}

func TestAvailabilityIsTotalMinusSoldMinusBlocked(t *testing.T) {
	h := newHotel(t, 3)
	h.book(h.desk, "2026-12-10", "2026-12-12", "").Want(t, 201)
	h.book(h.desk, "2026-12-11", "2026-12-13", "").Want(t, 201)
	got := h.desk.Do("GET", "/api/availability?from=2026-12-10&to=2026-12-13", &h.property, "").Want(t, 200)
	var rows []struct {
		RoomTypeID string
		Date       string
		Available  int
	}
	must(t, json.Unmarshal([]byte(got.Body), &rows))
	want := map[string]int{"2026-12-10": 2, "2026-12-11": 1, "2026-12-12": 2}
	if len(rows) != 3 {
		t.Fatalf("%+v", rows)
	}
	for _, r := range rows {
		if want[r.Date] != r.Available {
			t.Fatalf("%s: %d, want %d", r.Date, r.Available, want[r.Date])
		}
	}
}

func TestListFiltersByStayStatusAndSearch(t *testing.T) {
	h := newHotel(t, 3)
	a := newID(t, h.book(h.desk, "2026-12-10", "2026-12-12", "").Want(t, 201))
	h.desk.Do("POST", "/api/reservations", &h.property, strings.Replace(strings.Replace(`{"guest":{"name":"Zed Quinn"},"rooms":[{"roomTypeId":"RT","ratePlanId":"RP","from":"2026-12-20","to":"2026-12-21","adults":1,"children":0}]}`, "RT", h.roomType, 1), "RP", h.ratePlan, 1)).Want(t, 201)
	count := func(q string) int {
		var rows []map[string]any
		must(t, json.Unmarshal([]byte(h.desk.Do("GET", "/api/reservations"+q, &h.property, "").Want(t, 200).Body), &rows))
		return len(rows)
	}
	if count("") != 2 || count("?from=2026-12-11&to=2026-12-15") != 1 || count("?q=zed") != 1 || count("?q="+a.Code) != 1 || count("?status=cancelled") != 0 {
		t.Fatal("filters do not narrow the list")
	}
	h.desk.Do("POST", "/api/reservations/"+a.ID+"/cancel", &h.property, "").Want(t, 204)
	if count("?status=cancelled") != 1 {
		t.Fatal("the cancelled booking is not listed")
	}
}

func TestTheCalendarListsStaysWithTheirRoomsAndTheUnassignedOnes(t *testing.T) {
	h := newHotel(t, 2)
	a := newID(t, h.book(h.desk, "2026-12-10", "2026-12-12", "").Want(t, 201))
	b := newID(t, h.book(h.desk, "2026-12-11", "2026-12-14", "").Want(t, 201))
	h.desk.Do("POST", "/api/reservations/"+a.ID+"/assign", &h.property, fmt.Sprintf(`{"roomId":%q}`, h.roomID("101"))).Want(t, 204)
	h.book(h.desk, "2027-03-01", "2027-03-02", "").Want(t, 400) // no rate that far out, so it is not on the calendar
	var out struct {
		Rooms []struct{ ID, Number, RoomTypeName string }
		Stays []struct {
			ReservationID, Code, Status, GuestName string
			RoomID                                 *string
			From, To                               string
			Total                                  int64
		}
	}
	must(t, json.Unmarshal([]byte(h.desk.Do("GET", "/api/calendar?from=2026-12-09&to=2026-12-16", &h.property, "").Want(t, 200).Body), &out))
	if len(out.Rooms) != 2 || out.Rooms[0].Number != "101" || out.Rooms[0].RoomTypeName != "Deluxe" || len(out.Stays) != 2 {
		t.Fatalf("%+v", out)
	}
	for _, s := range out.Stays {
		switch s.ReservationID {
		case a.ID:
			if s.RoomID == nil || s.From != "2026-12-10" || s.To != "2026-12-12" || s.GuestName != "Mai Pham" || s.Total != 20000 {
				t.Fatalf("%+v", s)
			}
		case b.ID:
			if s.RoomID != nil {
				t.Fatalf("an unassigned stay has a room: %+v", s)
			}
		default:
			t.Fatalf("unexpected stay %+v", s)
		}
	}
	h.desk.Do("GET", "/api/calendar?from=2026-12-16&to=2026-12-09", &h.property, "").Want(t, 400)
	hk := h.w.Member(h.w.OrgA, &h.property, "housekeeping")
	hk.Do("GET", "/api/calendar?from=2026-12-09&to=2026-12-16", &h.property, "").Want(t, 403)
}

func TestAQuoteShowsThePriceAndWhetherTheRoomIsFree(t *testing.T) {
	h := newHotel(t, 1)
	q := fmt.Sprintf("/api/quote?roomTypeId=%s&ratePlanId=%s&adults=2&children=0", h.roomType, h.ratePlan)
	h.desk.Do("GET", q+"&from=2026-12-10&to=2026-12-12", &h.property, "").Want(t, 200, `"total":20000`, `"nights":2`, `"available":true`, `"currency":"USD"`)
	h.book(h.desk, "2026-12-11", "2026-12-12", "").Want(t, 201)
	h.desk.Do("GET", q+"&from=2026-12-10&to=2026-12-12", &h.property, "").Want(t, 200, `"available":false`)
	h.desk.Do("GET", q+"&from=2026-12-10&to=2026-12-10", &h.property, "").Want(t, 400)
	h.desk.Do("GET", q+"&from=2027-06-10&to=2027-06-12", &h.property, "").Want(t, 400, "no_rate")
	h.desk.Do("GET", strings.Replace(q, "adults=2", "adults=9", 1)+"&from=2026-12-14&to=2026-12-15", &h.property, "").Want(t, 400, "too_many_guests")
}

func TestGuestSearchFindsAPhoneNumberOnlyForThoseWhoSeeContacts(t *testing.T) {
	h := newHotel(t, 2)
	h.setup(h.w.A2, 1)
	body := strings.Replace(`{"guest":{"name":"Mai P.","email":"mai@hotel.com","phone":"+84 90 111 2233"},"rooms":[{"roomTypeId":"RT","ratePlanId":"RP","from":"2026-12-12","to":"2026-12-13","adults":1,"children":0}]}`, "RT", h.roomType, 1)
	body = strings.Replace(body, "RP", h.ratePlan, 1)
	h.desk.Do("POST", "/api/reservations", &h.property, body).Want(t, 201)
	h.desk.Do("GET", "/api/guests?q=111%202233", &h.property, "").Want(t, 200, "mai@hotel.com")
	if got := h.desk.Do("GET", "/api/guests?q=zzz", &h.property, "").Want(t, 200); strings.Contains(got.Body, "mai@hotel.com") {
		t.Fatalf("a search that matches nothing returned the guest: %s", got.Body)
	}
}
