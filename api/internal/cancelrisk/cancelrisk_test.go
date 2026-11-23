package cancelrisk_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/cancelrisk"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/rooms"
	"hotelpms/api/internal/testkit"
)

func TestEachSignalAddsItsWeightAndTheLevelFollowsTheSum(t *testing.T) {
	base := cancelrisk.Signals{LeadDays: 5, Source: "direct", Paid: true, Nights: 2}
	if r := cancelrisk.Score(base); r.Score != 0 || r.Level != "low" || len(r.Reasons) != 0 {
		t.Fatalf("a paid booking made close to the stay: %+v", r)
	}
	cases := []struct {
		name  string
		edit  func(*cancelrisk.Signals)
		score int
		code  string
	}{
		{"made 30 days ahead", func(s *cancelrisk.Signals) { s.LeadDays = 30 }, 15, "early"},
		{"made 60 days ahead", func(s *cancelrisk.Signals) { s.LeadDays = 60 }, 25, "very_early"},
		{"from a booking site", func(s *cancelrisk.Signals) { s.Source = "booking_com" }, 15, "booking_site"},
		{"nothing paid", func(s *cancelrisk.Signals) { s.Paid = false }, 20, "nothing_paid"},
		{"one earlier cancellation", func(s *cancelrisk.Signals) { s.PriorCancels = 1 }, 20, "past_cancel"},
		{"two earlier cancellations", func(s *cancelrisk.Signals) { s.PriorCancels = 2 }, 35, "past_cancels"},
	}
	for _, c := range cases {
		s := base
		c.edit(&s)
		r := cancelrisk.Score(s)
		if r.Score != c.score || len(r.Reasons) != 1 || r.Reasons[0].Code != c.code || r.Reasons[0].Text == "" {
			t.Errorf("%s: %+v", c.name, r)
		}
	}
	all := cancelrisk.Score(cancelrisk.Signals{LeadDays: 90, Source: "expedia", Paid: false, Nights: 2, PriorCancels: 2})
	if all.Score != 95 || all.Level != "high" {
		t.Fatalf("%+v", all)
	}
	if r := cancelrisk.Score(cancelrisk.Signals{LeadDays: 40, Source: "direct", Paid: false, Nights: 2}); r.Level != "medium" {
		t.Fatalf("35 should be medium: %+v", r)
	}
	// The website is a direct channel, and the front desk's own bookings are too.
	for _, src := range []string{"direct", "website"} {
		if r := cancelrisk.Score(cancelrisk.Signals{Source: src, Paid: true}); r.Score != 0 {
			t.Errorf("%s: %+v", src, r)
		}
	}
}

type hotel struct {
	w         *testkit.World
	prop      uuid.UUID
	mgr, desk *testkit.Person
	rt, rp    string
}

func idOf(t *testing.T, r testkit.Reply) string {
	t.Helper()
	var b struct{ ID string }
	testkit.Must(t, json.Unmarshal([]byte(r.Body), &b))
	return b.ID
}

func newHotel(t *testing.T) *hotel {
	w := testkit.New(t, rooms.Mount, booking.Mount, cancelrisk.Mount)
	h := &hotel{w: w, prop: w.A1}
	h.mgr = w.Member(w.OrgA, &w.A1, "manager")
	h.desk = w.Member(w.OrgA, &w.A1, "front_desk")
	h.rt = idOf(t, h.mgr.Do("POST", "/api/room-types", &h.prop, `{"name":"Deluxe","baseGuests":2,"maxGuests":3}`).Want(t, 201))
	for i := 0; i < 4; i++ {
		h.mgr.Do("POST", "/api/rooms", &h.prop, fmt.Sprintf(`{"roomTypeId":%q,"number":"%d","floor":1}`, h.rt, 101+i)).Want(t, 201)
	}
	h.rp = idOf(t, h.mgr.Do("POST", "/api/rate-plans", &h.prop, fmt.Sprintf(`{"roomTypeId":%q,"name":"Flexible"}`, h.rt)).Want(t, 201))
	h.mgr.Do("PUT", "/api/rates", &h.prop, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-11-23","to":"2027-03-01","price":10000}`, h.rp)).Want(t, 200)
	return h
}

func (h *hotel) book(t *testing.T, email, from, to string) string {
	t.Helper()
	return idOf(t, h.desk.Do("POST", "/api/reservations", &h.prop, fmt.Sprintf(`{"guest":{"name":"Mai Pham","email":%q},"rooms":[{"roomTypeId":%q,"ratePlanId":%q,"from":%q,"to":%q,"adults":2,"children":0}]}`, email, h.rt, h.rp, from, to)).Want(t, 201))
}

func (h *hotel) sql(t *testing.T, q string, args ...any) {
	t.Helper()
	testkit.Must(t, db.WithOrg(testkit.Ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error { _, err := tx.Exec(testkit.Ctx, q, args...); return err }))
}

func TestAReservationShowsItsRiskAndTheReasonsInPlainWords(t *testing.T) {
	h := newHotel(t)
	soon := h.book(t, "a@hotel.com", "2026-11-25", "2026-11-27")
	h.desk.Do("GET", "/api/reservations/"+soon+"/cancel-risk", &h.prop, "").Want(t, 200, `"score":20`, `"level":"low"`, "Nothing has been paid")

	far := h.book(t, "b@hotel.com", "2027-02-10", "2027-02-12")
	h.sql(t, `update reservation set source_channel = 'booking_com' where id = $1`, uuid.MustParse(far))
	h.desk.Do("GET", "/api/reservations/"+far+"/cancel-risk", &h.prop, "").Want(t, 200, `"score":60`, `"level":"high"`, "booking site", "ahead")
}

func TestEarlierCancellationsByTheSameGuestRaiseTheScore(t *testing.T) {
	h := newHotel(t)
	first := h.book(t, "c@hotel.com", "2026-12-10", "2026-12-12")
	h.desk.Do("POST", "/api/reservations/"+first+"/cancel", &h.prop, "").Want(t, 204)
	again := h.book(t, "C@hotel.com", "2026-12-10", "2026-12-12")
	h.desk.Do("GET", "/api/reservations/"+again+"/cancel-risk", &h.prop, "").Want(t, 200, `"score":40`, "earlier booking")
}

func TestTheUpcomingListPutsTheRiskiestFirstAndSkipsTheRest(t *testing.T) {
	h := newHotel(t)
	h.book(t, "a@hotel.com", "2026-11-25", "2026-11-27")
	risky := h.book(t, "b@hotel.com", "2026-12-05", "2026-12-07")
	h.sql(t, `update reservation set source_channel = 'expedia' where id = $1`, uuid.MustParse(risky))
	h.book(t, "far@hotel.com", "2027-02-20", "2027-02-22")
	var list []struct {
		ReservationID string
		Score         int
	}
	testkit.Must(t, json.Unmarshal([]byte(h.desk.Do("GET", "/api/cancel-risk?days=30", &h.prop, "").Want(t, 200).Body), &list))
	if len(list) != 2 || list[0].ReservationID != risky || list[0].Score != 35 || list[1].Score != 20 {
		t.Fatalf("%+v", list)
	}
}

func TestAnotherPropertyCannotReadTheRisk(t *testing.T) {
	h := newHotel(t)
	res := h.book(t, "a@hotel.com", "2026-11-25", "2026-11-27")
	other := h.w.A2
	owner := h.w.Member(h.w.OrgA, nil, "owner")
	owner.Do("GET", "/api/reservations/"+res+"/cancel-risk", &other, "").Want(t, 404)
	h.w.Member(h.w.OrgA, &h.w.A1, "housekeeping").Do("GET", "/api/cancel-risk", &h.prop, "").Want(t, 403)
}

func TestOverbookingHintsCountExpectedCancellationsOnFullNightsOnly(t *testing.T) {
	h := newHotel(t)
	hints := func(days int) (out []struct {
		Date, RoomType                string
		Rooms, Booked, SuggestedExtra int
		Expected                      float64
	}) {
		testkit.Must(t, json.Unmarshal([]byte(h.mgr.Do("GET", fmt.Sprintf("/api/overbooking-hints?days=%d", days), &h.prop, "").Want(t, 200).Body), &out))
		return out
	}
	h.desk.Do("GET", "/api/overbooking-hints", &h.prop, "").Want(t, 403)
	for i := 0; i < 3; i++ { // three of four rooms is not full enough
		id := h.book(t, fmt.Sprintf("far%d@hotel.com", i), "2027-02-10", "2027-02-11")
		h.sql(t, `update reservation set source_channel = 'expedia' where id = $1`, uuid.MustParse(id))
	}
	if got := hints(100); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
	id := h.book(t, "far3@hotel.com", "2027-02-10", "2027-02-11")
	h.sql(t, `update reservation set source_channel = 'expedia' where id = $1`, uuid.MustParse(id))
	got := hints(100)
	if len(got) != 1 || got[0].Date != "2027-02-10" || got[0].RoomType != "Deluxe" || got[0].Rooms != 4 || got[0].Booked != 4 || got[0].SuggestedExtra != 1 || got[0].Expected < 1.19 || got[0].Expected > 1.21 {
		t.Fatalf("%+v", got)
	}
	// Four near bookings with little risk expect almost no cancellations.
	for i := 0; i < 4; i++ {
		h.book(t, fmt.Sprintf("near%d@hotel.com", i), "2026-11-25", "2026-11-26")
	}
	for _, g := range hints(10) {
		t.Errorf("a low risk night has a hint: %+v", g)
	}
}
