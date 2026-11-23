package risk_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/alerts"
	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/risk"
	"hotelpms/api/internal/rooms"
	"hotelpms/api/internal/testkit"
)

func TestAnOrdinaryBookingIsLowRiskAndSaysNothing(t *testing.T) {
	r := risk.Score(risk.Signals{Email: "mai@hotel.com", Name: "Mai Pham", Nights: 2, DaysToArrival: 30, Card: true})
	if r.Level != "low" || r.Score != 0 || len(r.Reasons) != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestEachSignalAddsItsOwnWeightAndTheLevelFollowsTheSum(t *testing.T) {
	base := risk.Signals{Email: "mai@hotel.com", Name: "Mai Pham", Nights: 2, DaysToArrival: 30, Card: true}
	cases := []struct {
		name  string
		edit  func(*risk.Signals)
		score int
		level string
		code  string
	}{
		{"a throwaway email address", func(s *risk.Signals) { s.Email = "x@Mailinator.com" }, 40, "medium", "disposable_email"},
		{"three bookings from one address in an hour", func(s *risk.Signals) { s.FromAddressLastHour = 3 }, 30, "medium", "many_from_address"},
		{"three bookings for one email in a day", func(s *risk.Signals) { s.ForEmailLastDay = 3 }, 25, "low", "many_for_email"},
		{"a card booking for tomorrow", func(s *risk.Signals) { s.DaysToArrival = 1 }, 10, "low", "last_minute_card"},
		{"a very long stay", func(s *risk.Signals) { s.Nights = 14 }, 10, "low", "long_stay"},
		{"a name with no letters", func(s *risk.Signals) { s.Name = "1234" }, 15, "low", "odd_name"},
	}
	for _, c := range cases {
		s := base
		c.edit(&s)
		r := risk.Score(s)
		if r.Score != c.score || r.Level != c.level || len(r.Reasons) != 1 || r.Reasons[0].Code != c.code || r.Reasons[0].Text == "" {
			t.Errorf("%s: %+v", c.name, r)
		}
	}
	s := base
	s.Email, s.ForEmailLastDay, s.FromAddressLastHour = "x@yopmail.com", 3, 3
	if r := risk.Score(s); r.Level != "high" || r.Score != 95 || len(r.Reasons) != 3 {
		t.Fatalf("%+v", r)
	}
	// Tomorrow only counts for a card, because a hotel loses nothing when a guest pays at the desk.
	s = base
	s.DaysToArrival, s.Card = 1, false
	if r := risk.Score(s); r.Score != 0 {
		t.Fatalf("%+v", r)
	}
}

type hotel struct {
	w                *testkit.World
	prop             uuid.UUID
	owner, mgr, desk *testkit.Person
	rt, rp           string
}

func idOf(t *testing.T, r testkit.Reply) string {
	t.Helper()
	var b struct{ ID string }
	testkit.Must(t, json.Unmarshal([]byte(r.Body), &b))
	return b.ID
}

func newHotel(t *testing.T) *hotel {
	w := testkit.New(t, rooms.Mount, booking.Mount, risk.Mount)
	h := &hotel{w: w, prop: w.A1}
	h.owner = w.Member(w.OrgA, nil, "owner")
	h.mgr = w.Member(w.OrgA, &w.A1, "manager")
	h.desk = w.Member(w.OrgA, &w.A1, "front_desk")
	h.rt = idOf(t, h.mgr.Do("POST", "/api/room-types", &h.prop, `{"name":"Deluxe","baseGuests":2,"maxGuests":3}`).Want(t, 201))
	for i := 0; i < 2; i++ {
		h.mgr.Do("POST", "/api/rooms", &h.prop, fmt.Sprintf(`{"roomTypeId":%q,"number":"%d","floor":1}`, h.rt, 101+i)).Want(t, 201)
	}
	h.rp = idOf(t, h.mgr.Do("POST", "/api/rate-plans", &h.prop, fmt.Sprintf(`{"roomTypeId":%q,"name":"Flexible"}`, h.rt)).Want(t, 201))
	h.mgr.Do("PUT", "/api/rates", &h.prop, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-11-23","to":"2027-01-01","price":10000}`, h.rp)).Want(t, 200)
	return h
}

// flagged books a stay and records a high risk against it, as the guest booking page does.
func (h *hotel) flagged(t *testing.T, level string) (res string) {
	t.Helper()
	res = idOf(t, h.desk.Do("POST", "/api/reservations", &h.prop, fmt.Sprintf(`{"guest":{"name":"Mai Pham","email":"mai@hotel.com"},"rooms":[{"roomTypeId":%q,"ratePlanId":%q,"from":"2026-12-10","to":"2026-12-12","adults":2,"children":0}]}`, h.rt, h.rp)).Want(t, 201))
	testkit.Must(t, db.WithOrg(testkit.Ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
		return risk.Record(testkit.Ctx, tx, h.w.OrgA, h.prop, uuid.MustParse(res), "ip", "mail", risk.Result{Score: 95, Level: level, Reasons: []risk.Reason{{Code: "disposable_email", Text: "The email address is a throwaway one."}}}, h.w.Now)
	}))
	return res
}

func TestOnlyManagersSeeAndDecideTheFlaggedBookings(t *testing.T) {
	h := newHotel(t)
	res := h.flagged(t, "high")
	h.desk.Do("GET", "/api/risk", &h.prop, "").Want(t, 403)
	h.desk.Do("POST", "/api/risk/"+res+"/approve", &h.prop, "").Want(t, 403)
	h.mgr.Do("GET", "/api/risk", &h.prop, "").Want(t, 200, res, "throwaway", `"level":"high"`, `"status":"review"`)
	h.mgr.Do("POST", "/api/risk/"+res+"/approve", &h.prop, "").Want(t, 204)
	h.mgr.Do("POST", "/api/risk/"+res+"/approve", &h.prop, "").Want(t, 409)
	body := h.mgr.Do("GET", "/api/risk", &h.prop, "").Want(t, 200).Body
	if body != "[]" && body != "[]\n" {
		t.Fatalf("a decided booking is still waiting: %s", body)
	}
	h.mgr.Do("GET", "/api/risk?all=1", &h.prop, "").Want(t, 200, `"status":"approved"`)
}

func TestDecliningAFlaggedBookingCancelsItAndFreesTheRoom(t *testing.T) {
	h := newHotel(t)
	res := h.flagged(t, "high")
	got := func(q string, args ...any) (out string) {
		testkit.Must(t, db.WithOrg(testkit.Ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error { return tx.QueryRow(testkit.Ctx, q, args...).Scan(&out) }))
		return out
	}
	if n := got(`select sold::text from inventory where date = '2026-12-10'`); n != "1" {
		t.Fatalf("sold %s", n)
	}
	h.mgr.Do("POST", "/api/risk/"+res+"/decline", &h.prop, "").Want(t, 204)
	if s := got(`select status from reservation where id = $1`, uuid.MustParse(res)); s != "cancelled" {
		t.Fatalf("%s", s)
	}
	if n := got(`select sold::text from inventory where date = '2026-12-10'`); n != "0" {
		t.Fatalf("the room was not freed: sold %s", n)
	}
	h.mgr.Do("GET", "/api/risk?all=1", &h.prop, "").Want(t, 200, `"status":"declined"`)
}

func TestAnotherPropertyCannotDecideThisPropertysFlaggedBooking(t *testing.T) {
	h := newHotel(t)
	res := h.flagged(t, "high")
	other := h.w.A2
	h.owner.Do("POST", "/api/risk/"+res+"/decline", &other, "").Want(t, 404)
	h.mgr.Do("GET", "/api/risk", &h.prop, "").Want(t, 200, res)
}

func TestCountsLookAtTheLastHourAndDayOnly(t *testing.T) {
	h := newHotel(t)
	res := h.flagged(t, "low")
	testkit.Must(t, db.WithOrg(testkit.Ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
		a, e, err := risk.Counts(testkit.Ctx, tx, h.prop, "ip", "mail", h.w.Now)
		if err != nil || a != 1 || e != 1 {
			t.Fatalf("now: %d %d %v", a, e, err)
		}
		a, e, err = risk.Counts(testkit.Ctx, tx, h.prop, "ip", "mail", h.w.Now.Add(90*time.Minute))
		if err != nil || a != 0 || e != 1 {
			t.Fatalf("after 90 minutes: %d %d %v", a, e, err)
		}
		a, e, err = risk.Counts(testkit.Ctx, tx, h.prop, "ip", "mail", h.w.Now.Add(25*time.Hour))
		if err != nil || a != 0 || e != 0 {
			t.Fatalf("after a day: %d %d %v", a, e, err)
		}
		return nil
	}))
	_ = res
}

func TestAHighRiskBookingRaisesOneAlertForManagersUntilItIsDecided(t *testing.T) {
	h := newHotel(t)
	res := h.flagged(t, "high")
	open := func() (n string) {
		testkit.Must(t, alerts.Run(testkit.Ctx, h.w.Pool, h.w.Now))
		testkit.Must(t, db.WithOrg(testkit.Ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
			return tx.QueryRow(testkit.Ctx, `select count(*)::text from alert where check_kind = 'booking_review' and status = 'open'`).Scan(&n)
		}))
		return n
	}
	if n := open(); n != "1" {
		t.Fatalf("%s alerts", n)
	}
	if n := open(); n != "1" {
		t.Fatalf("a second run made %s alerts", n)
	}
	h.mgr.Do("POST", "/api/risk/"+res+"/approve", &h.prop, "").Want(t, 204)
	if n := open(); n != "0" {
		t.Fatalf("the alert stayed after the decision: %s", n)
	}
}
