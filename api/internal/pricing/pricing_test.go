package pricing_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/pricing"
	"hotelpms/api/internal/rooms"
	"hotelpms/api/internal/testkit"
)

func ptr(n int64) *int64 { return &n }

func TestTheSuggestionFollowsOccupancyCompetitorsAndTheLimits(t *testing.T) {
	cases := []struct {
		name string
		in   pricing.Input
		want int64 // 0 means no suggestion
		text string
	}{
		{"nearly full", pricing.Input{Current: 10000, Occupancy: 0.9, DaysAhead: 30}, 11000, "90 percent"},
		{"busy", pricing.Input{Current: 10000, Occupancy: 0.75, DaysAhead: 30}, 10500, "75 percent"},
		{"quiet and close", pricing.Input{Current: 10000, Occupancy: 0.2, DaysAhead: 10}, 9500, "20 percent"},
		{"empty and close", pricing.Input{Current: 10000, Occupancy: 0.1, DaysAhead: 10}, 9000, "10 percent"},
		{"empty but far away", pricing.Input{Current: 10000, Occupancy: 0.1, DaysAhead: 60}, 0, ""},
		{"competitors charge more", pricing.Input{Current: 10000, Occupancy: 0.5, DaysAhead: 30, Competitor: ptr(12000)}, 10600, "competitors"},
		{"competitors charge less", pricing.Input{Current: 10000, Occupancy: 0.5, DaysAhead: 30, Competitor: ptr(8000)}, 9400, "competitors"},
		{"too small a change", pricing.Input{Current: 10000, Occupancy: 0.5, DaysAhead: 30, Competitor: ptr(10100)}, 0, ""},
		{"held by the ceiling", pricing.Input{Current: 10000, Occupancy: 0.9, DaysAhead: 30, Ceiling: ptr(10800)}, 10800, "ceiling"},
		{"held by the floor", pricing.Input{Current: 10000, Occupancy: 0.1, DaysAhead: 10, Floor: ptr(9800)}, 9800, "floor"},
		{"a currency with no cents", pricing.Input{Current: 1000000, Occupancy: 0.9, DaysAhead: 30, Currency: "VND"}, 1100000, "90 percent"},
	}
	for _, c := range cases {
		s, ok := pricing.Suggest(c.in)
		if c.want == 0 {
			if ok {
				t.Errorf("%s: a suggestion %+v", c.name, s)
			}
			continue
		}
		joined := ""
		for _, r := range s.Reasons {
			joined += r + " "
		}
		if !ok || s.Price != c.want || !strings.Contains(joined, c.text) {
			t.Errorf("%s: got %+v ok %v, want %d and %q", c.name, s, ok, c.want, c.text)
		}
	}
}

func TestACompetitorMedianIgnoresOneOddPrice(t *testing.T) {
	if m, ok := pricing.Median([]int64{9000, 9500, 30000}); !ok || m != 9500 {
		t.Fatalf("%d %v", m, ok)
	}
	if m, ok := pricing.Median([]int64{9000, 10000}); !ok || m != 9500 {
		t.Fatalf("%d %v", m, ok)
	}
	if _, ok := pricing.Median(nil); ok {
		t.Fatal("no prices have no median")
	}
}

type hotel struct {
	w                 *testkit.World
	prop              uuid.UUID
	mgr, desk, keeper *testkit.Person
	rt, rp            string
}

func idOf(t *testing.T, r testkit.Reply) string {
	t.Helper()
	var b struct{ ID string }
	testkit.Must(t, json.Unmarshal([]byte(r.Body), &b))
	return b.ID
}

func newHotel(t *testing.T) *hotel {
	w := testkit.New(t, rooms.Mount, booking.Mount, pricing.Mount)
	h := &hotel{w: w, prop: w.A1}
	h.mgr = w.Member(w.OrgA, &w.A1, "manager")
	h.desk = w.Member(w.OrgA, &w.A1, "front_desk")
	h.keeper = w.Member(w.OrgA, &w.A1, "housekeeping")
	h.rt = idOf(t, h.mgr.Do("POST", "/api/room-types", &h.prop, `{"name":"Deluxe","baseGuests":2,"maxGuests":3}`).Want(t, 201))
	for i := 0; i < 4; i++ {
		h.mgr.Do("POST", "/api/rooms", &h.prop, fmt.Sprintf(`{"roomTypeId":%q,"number":"%d","floor":1}`, h.rt, 101+i)).Want(t, 201)
	}
	h.rp = idOf(t, h.mgr.Do("POST", "/api/rate-plans", &h.prop, fmt.Sprintf(`{"roomTypeId":%q,"name":"Flexible"}`, h.rt)).Want(t, 201))
	h.mgr.Do("PUT", "/api/rates", &h.prop, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-11-23","to":"2027-03-01","price":10000}`, h.rp)).Want(t, 200)
	return h
}

func (h *hotel) book(t *testing.T, n int, from, to string) {
	for i := 0; i < n; i++ {
		h.desk.Do("POST", "/api/reservations", &h.prop, fmt.Sprintf(`{"guest":{"name":"Guest %d"},"rooms":[{"roomTypeId":%q,"ratePlanId":%q,"from":%q,"to":%q,"adults":2,"children":0}]}`, i, h.rt, h.rp, from, to)).Want(t, 201)
	}
}

type suggestion struct {
	Date, RatePlanID, RoomType string
	Current, Price             int64
	Occupancy                  float64
	Reasons                    []string
}

func (h *hotel) suggestions(t *testing.T) (out []suggestion) {
	t.Helper()
	testkit.Must(t, json.Unmarshal([]byte(h.mgr.Do("GET", "/api/price-suggestions?days=30", &h.prop, "").Want(t, 200).Body), &out))
	return out
}

func TestSuggestionsReadOccupancyAndCompetitorsAndStaffApproveThem(t *testing.T) {
	h := newHotel(t)
	h.book(t, 3, "2026-12-10", "2026-12-11") // 3 of 4 rooms, 75 percent
	h.desk.Do("GET", "/api/price-suggestions", &h.prop, "").Want(t, 403)
	h.keeper.Do("GET", "/api/price-suggestions", &h.prop, "").Want(t, 403)
	h.mgr.Do("POST", "/api/competitor-rates", &h.prop, `{"rates":[{"date":"2026-12-15","name":"Sea Hotel","price":12000},{"date":"2026-12-15","name":"Bay Inn","price":12000},{"date":"2026-12-15","name":"Odd Place","price":40000}]}`).Want(t, 204)
	got := map[string]suggestion{}
	for _, s := range h.suggestions(t) {
		got[s.Date] = s
	}
	if s := got["2026-12-10"]; s.Price != 10500 || s.Current != 10000 || s.Occupancy != 0.75 || s.RoomType != "Deluxe" {
		t.Fatalf("a busy night: %+v", s)
	}
	if s := got["2026-12-15"]; s.Price != 10600 || len(s.Reasons) == 0 {
		t.Fatalf("a night with competitors: %+v", s)
	}
	if _, there := got["2026-12-20"]; there {
		t.Fatalf("a quiet night far away has a suggestion: %+v", got["2026-12-20"])
	}
	body := fmt.Sprintf(`{"changes":[{"ratePlanId":%q,"date":"2026-12-10","price":10500},{"ratePlanId":%q,"date":"2026-12-15","price":10600}]}`, h.rp, h.rp)
	h.desk.Do("POST", "/api/price-suggestions/apply", &h.prop, body).Want(t, 403)
	h.mgr.Do("POST", "/api/price-suggestions/apply", &h.prop, body).Want(t, 204)
	if s, there := map[string]suggestion{}[""]; there {
		_ = s
	}
	for _, s := range h.suggestions(t) {
		if s.Date == "2026-12-10" || s.Date == "2026-12-15" {
			t.Errorf("%s is still suggested after it was applied: %+v", s.Date, s)
		}
	}
	h.mgr.Do("GET", "/api/rates?ratePlanId="+h.rp+"&from=2026-12-10&to=2026-12-11", &h.prop, "").Want(t, 200, "10500")
}

func TestTheLimitsHoldSuggestionsAndApplyingBeyondThemIsRefused(t *testing.T) {
	h := newHotel(t)
	h.book(t, 4, "2026-12-10", "2026-12-11") // full: +10 percent
	h.mgr.Do("PUT", "/api/price-limits", &h.prop, fmt.Sprintf(`{"roomTypeId":%q,"floor":9000,"ceiling":10800}`, h.rt)).Want(t, 204)
	h.mgr.Do("PUT", "/api/price-limits", &h.prop, fmt.Sprintf(`{"roomTypeId":%q,"floor":9000,"ceiling":8000}`, h.rt)).Want(t, 400)
	h.mgr.Do("PUT", "/api/price-limits", &h.prop, fmt.Sprintf(`{"roomTypeId":%q,"floor":9000}`, uuid.NewString())).Want(t, 404)
	for _, s := range h.suggestions(t) {
		if s.Date == "2026-12-10" && (s.Price != 10800 || !strings.Contains(strings.Join(s.Reasons, " "), "ceiling")) {
			t.Fatalf("%+v", s)
		}
	}
	h.mgr.Do("POST", "/api/price-suggestions/apply", &h.prop, fmt.Sprintf(`{"changes":[{"ratePlanId":%q,"date":"2026-12-10","price":11000}]}`, h.rp)).Want(t, 400)
	h.mgr.Do("POST", "/api/price-suggestions/apply", &h.prop, fmt.Sprintf(`{"changes":[{"ratePlanId":%q,"date":"2026-12-10","price":0}]}`, h.rp)).Want(t, 400)
	h.mgr.Do("POST", "/api/price-suggestions/apply", &h.prop, fmt.Sprintf(`{"changes":[{"ratePlanId":%q,"date":"2026-12-10","price":10800}]}`, uuid.NewString())).Want(t, 404)
}

func TestCompetitorPricesAreListedAndRemovedPerProperty(t *testing.T) {
	h := newHotel(t)
	h.mgr.Do("POST", "/api/competitor-rates", &h.prop, `{"rates":[{"date":"2026-12-15","name":"Sea Hotel","price":12000}]}`).Want(t, 204)
	h.mgr.Do("POST", "/api/competitor-rates", &h.prop, `{"rates":[{"date":"2026-12-15","name":"Sea Hotel","price":12500}]}`).Want(t, 204) // the same hotel and night is replaced
	var list []struct {
		ID    string
		Price int64
	}
	testkit.Must(t, json.Unmarshal([]byte(h.mgr.Do("GET", "/api/competitor-rates?from=2026-12-01&to=2026-12-31", &h.prop, "").Want(t, 200).Body), &list))
	if len(list) != 1 || list[0].Price != 12500 {
		t.Fatalf("%+v", list)
	}
	for _, bad := range []string{`{"rates":[{"date":"tomorrow","name":"X","price":100}]}`, `{"rates":[{"date":"2026-12-15","name":"","price":100}]}`, `{"rates":[{"date":"2026-12-15","name":"X","price":0}]}`, `{"rates":[]}`} {
		h.mgr.Do("POST", "/api/competitor-rates", &h.prop, bad).Want(t, 400)
	}
	other := h.w.A2
	owner := h.w.Member(h.w.OrgA, nil, "owner")
	owner.Do("DELETE", "/api/competitor-rates/"+list[0].ID, &other, "").Want(t, 404)
	h.mgr.Do("DELETE", "/api/competitor-rates/"+list[0].ID, &h.prop, "").Want(t, 204)
}
