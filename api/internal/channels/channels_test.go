package channels_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/channels"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/folio"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/rooms"
	"hotelpms/api/internal/testkit"
)

type hotel struct {
	w                    *testkit.World
	fake                 *channels.Fake
	owner, manager, desk *testkit.Person
	rt, rp               string
}

func idOf(t *testing.T, r testkit.Reply) string {
	t.Helper()
	var b struct{ ID string }
	testkit.Must(t, json.Unmarshal([]byte(r.Body), &b))
	return b.ID
}

func newHotel(t *testing.T, nRooms int) *hotel {
	fake := channels.NewFake()
	w := testkit.New(t, rooms.Mount, booking.Mount, folio.Mount, channels.Mount(channels.Fixed(fake)))
	h := &hotel{w: w, fake: fake}
	h.owner = w.Member(w.OrgA, nil, "owner")
	h.manager = w.Member(w.OrgA, &w.A1, "manager")
	h.desk = w.Member(w.OrgA, &w.A1, "front_desk")
	h.rt = idOf(t, h.manager.Do("POST", "/api/room-types", &w.A1, `{"name":"Deluxe","baseGuests":2,"maxGuests":3}`).Want(t, 201))
	for i := 0; i < nRooms; i++ {
		h.manager.Do("POST", "/api/rooms", &w.A1, fmt.Sprintf(`{"roomTypeId":%q,"number":"%d","floor":1}`, h.rt, 101+i)).Want(t, 201)
	}
	h.rp = idOf(t, h.manager.Do("POST", "/api/rate-plans", &w.A1, fmt.Sprintf(`{"roomTypeId":%q,"name":"Flexible"}`, h.rt)).Want(t, 201))
	h.manager.Do("PUT", "/api/rates", &w.A1, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-11-23","to":"2027-01-01","price":10000}`, h.rp)).Want(t, 200)
	return h
}

func (h *hotel) connect(t *testing.T, mode, provider string, enabled bool) {
	h.manager.Do("PUT", "/api/channel", &h.w.A1, fmt.Sprintf(`{"mode":%q,"provider":%q,"externalHotelId":"H1","enabled":%v}`, mode, provider, enabled)).Want(t, 200)
	h.manager.Do("PUT", "/api/channel/map", &h.w.A1, fmt.Sprintf(`{"ratePlanId":%q,"externalRoomId":"R1","externalRateId":"P1"}`, h.rp)).Want(t, 200)
}

func (h *hotel) row(t *testing.T, q string, args ...any) (out string) {
	t.Helper()
	testkit.Must(t, db.WithOrg(testkit.Ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error { return tx.QueryRow(testkit.Ctx, q, args...).Scan(&out) }))
	return out
}

var _ httpx.Mount = channels.Mount(nil)

func TestOnlyChannelPeopleSetTheConnectionAndTheModeMustMatchTheProvider(t *testing.T) {
	h := newHotel(t, 1)
	body := `{"mode":"direct","provider":"agoda","externalHotelId":"H1","secretSetting":"AGODA_KEY","enabled":false}`
	h.desk.Do("PUT", "/api/channel", &h.w.A1, body).Want(t, 403)
	h.manager.Do("PUT", "/api/channel", &h.w.A1, `{"mode":"direct","provider":"cm_generic"}`).Want(t, 400)     // a channel manager is not an OTA
	h.manager.Do("PUT", "/api/channel", &h.w.A1, `{"mode":"channel_manager","provider":"agoda"}`).Want(t, 400) // and an OTA is not a channel manager
	h.manager.Do("PUT", "/api/channel", &h.w.A1, `{"mode":"direct","provider":"agoda","baseUrl":"file:///etc/passwd"}`).Want(t, 400)
	h.manager.Do("PUT", "/api/channel", &h.w.A1, body).Want(t, 200)
	got := h.manager.Do("GET", "/api/channel", &h.w.A1, "").Want(t, 200, `"provider":"agoda"`).Body
	if strings.Contains(got, "AGODA_KEY") {
		t.Fatalf("the secret setting name is shown back: %s", got)
	}
	h.manager.Do("PUT", "/api/channel", &h.w.A1, `{"mode":"channel_manager","provider":"cm_generic","enabled":false}`).Want(t, 200) // the choice can change
}

func TestAnEnabledConnectionPushesWhatIsLeftToSellAndAClosedDayIsZero(t *testing.T) {
	h := newHotel(t, 2)
	h.connect(t, "channel_manager", "fake", true)
	h.manager.Do("PUT", "/api/rates", &h.w.A1, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-12-11","to":"2026-12-12","price":12000,"closed":true}`, h.rp)).Want(t, 200)
	h.manager.Do("POST", "/api/reservations", &h.w.A1, fmt.Sprintf(`{"guest":{"name":"Mai Pham"},"rooms":[{"roomTypeId":%q,"ratePlanId":%q,"from":"2026-12-10","to":"2026-12-11","adults":2,"children":0}]}`, h.rt, h.rp)).Want(t, 201)
	h.desk.Do("POST", "/api/channel/sync", &h.w.A1, "").Want(t, 403)
	h.manager.Do("POST", "/api/channel/sync", &h.w.A1, "").Want(t, 200)
	by := map[string]channels.Slot{}
	for _, s := range h.fake.Pushed {
		by[s.Date] = s
	}
	if s := by["2026-12-10"]; s.Available != 1 || s.Price != 10000 || s.ExternalRoomID != "R1" || s.ExternalRateID != "P1" {
		t.Fatalf("%+v", s)
	}
	if by["2026-12-09"].Available != 2 || by["2026-12-11"].Available != 0 {
		t.Fatalf("%+v %+v", by["2026-12-09"], by["2026-12-11"])
	}
}

func TestADisabledConnectionPushesNothing(t *testing.T) {
	h := newHotel(t, 1)
	h.connect(t, "channel_manager", "fake", false)
	h.manager.Do("POST", "/api/channel/sync", &h.w.A1, "").Want(t, 409, "channel_off")
	if len(h.fake.Pushed) != 0 {
		t.Fatal("pushed while off")
	}
}

func TestPulledBookingsBecomeReservationsOnceAndCancellationsFreeTheRoom(t *testing.T) {
	h := newHotel(t, 1)
	h.connect(t, "direct", "fake", true)
	b := channels.Booking{ExternalID: "EXT-1", Status: "new", GuestName: "Kim Park", Email: "kim@example.com", Arrival: "2026-12-10", Departure: "2026-12-12", ExternalRoomID: "R1", ExternalRateID: "P1", Adults: 2}
	h.fake.Inbox = []channels.Booking{b}
	h.manager.Do("POST", "/api/channel/sync", &h.w.A1, "").Want(t, 200, `"pulled":1`)
	h.manager.Do("POST", "/api/channel/sync", &h.w.A1, "").Want(t, 200, `"pulled":0`) // the same booking again is not a second booking
	if got := h.row(t, `select count(*)::text from reservation where source_channel = 'fake' and channel_booking_ref = 'EXT-1'`); got != "1" {
		t.Fatalf("%s reservations", got)
	}
	if got := h.row(t, `select sold::text from inventory where date = '2026-12-10'`); got != "1" {
		t.Fatalf("sold %s", got)
	}
	b.Status = "cancelled"
	h.fake.Inbox = []channels.Booking{b}
	h.manager.Do("POST", "/api/channel/sync", &h.w.A1, "").Want(t, 200)
	if got := h.row(t, `select status from reservation where channel_booking_ref = 'EXT-1'`); got != "cancelled" {
		t.Fatalf("%s", got)
	}
	if got := h.row(t, `select sold::text from inventory where date = '2026-12-10'`); got != "0" {
		t.Fatalf("sold %s", got)
	}
}

func TestABookingThatCannotBeKeptIsLoggedForStaffAndTheRestStillComeIn(t *testing.T) {
	h := newHotel(t, 1)
	h.connect(t, "direct", "fake", true)
	h.fake.Inbox = []channels.Booking{
		{ExternalID: "A", Status: "new", GuestName: "One", Arrival: "2026-12-10", Departure: "2026-12-12", ExternalRoomID: "R1", ExternalRateID: "P1", Adults: 2},
		{ExternalID: "B", Status: "new", GuestName: "Two", Arrival: "2026-12-10", Departure: "2026-12-12", ExternalRoomID: "R1", ExternalRateID: "P1", Adults: 2}, // the only room is taken
		{ExternalID: "C", Status: "new", GuestName: "Three", Arrival: "2026-12-20", Departure: "2026-12-21", ExternalRoomID: "NOPE", Adults: 2},                   // not mapped
		{ExternalID: "D", Status: "new", GuestName: "Four", Arrival: "2026-12-25", Departure: "2026-12-26", ExternalRoomID: "R1", ExternalRateID: "P1", Adults: 2},
	}
	h.manager.Do("POST", "/api/channel/sync", &h.w.A1, "").Want(t, 200, `"pulled":2`)
	log := h.manager.Do("GET", "/api/channel/log", &h.w.A1, "").Want(t, 200).Body
	for _, want := range []string{`"status":"error"`, "B", "C"} {
		if !strings.Contains(log, want) {
			t.Fatalf("log lacks %q: %s", want, log)
		}
	}
	h.desk.Do("GET", "/api/channel/log", &h.w.A1, "").Want(t, 403)
}

func TestTheBackgroundRunSyncsEveryEnabledConnectionAndSkipsTheOthers(t *testing.T) {
	h := newHotel(t, 1)
	h.connect(t, "direct", "fake", true)
	n, err := channels.RunAll(testkit.Ctx, h.w.Pool, channels.Fixed(h.fake), h.w.Now)
	testkit.Must(t, err)
	if n != 1 || len(h.fake.Pushed) == 0 {
		t.Fatalf("%d connections synced, %d nights pushed", n, len(h.fake.Pushed))
	}
	h.fake.Pushed = nil
	h.manager.Do("PUT", "/api/channel", &h.w.A1, `{"mode":"direct","provider":"fake","externalHotelId":"H1","enabled":false}`).Want(t, 200)
	n, err = channels.RunAll(testkit.Ctx, h.w.Pool, channels.Fixed(h.fake), h.w.Now)
	testkit.Must(t, err)
	if n != 0 || len(h.fake.Pushed) != 0 {
		t.Fatalf("a switched-off connection was synced: %d, %d", n, len(h.fake.Pushed))
	}
}

func TestTheSetupListsProvidersAndTheRecommendedOneComesFirst(t *testing.T) {
	h := newHotel(t, 1)
	got := h.manager.Do("GET", "/api/channel/providers", &h.w.A1, "").Want(t, 200).Body
	var ps []struct {
		ID, Mode, Name string
		Recommended    bool
		Connectable    bool
		Fields         []string
	}
	testkit.Must(t, json.Unmarshal([]byte(got), &ps))
	if len(ps) < 5 || ps[0].ID != "channex" || !ps[0].Recommended || ps[0].Mode != "channel_manager" {
		t.Fatalf("%+v", ps)
	}
	seen := map[string]bool{}
	for _, p := range ps {
		seen[p.ID] = true
		if p.ID == "fake" {
			t.Fatal("the test provider is offered to owners")
		}
		if p.Connectable && len(p.Fields) == 0 {
			t.Errorf("%s names no fields to fill in", p.ID)
		}
	}
	for _, id := range []string{"channex", "cm_generic", "agoda", "booking_com", "expedia", "google"} {
		if !seen[id] {
			t.Errorf("%s is missing", id)
		}
	}
	h.desk.Do("GET", "/api/channel/providers", &h.w.A1, "").Want(t, 403)
}

func TestTestingTheConnectionSaysWhyItFailsAndNeedsNoSave(t *testing.T) {
	h := newHotel(t, 1)
	h.connect(t, "channel_manager", "fake", false)
	h.manager.Do("POST", "/api/channel/test", &h.w.A1, "").Want(t, 200, `"ok":true`)
	h.fake.TestErr = errors.New("the key was refused")
	h.manager.Do("POST", "/api/channel/test", &h.w.A1, "").Want(t, 200, `"ok":false`, "the key was refused")
	h.desk.Do("POST", "/api/channel/test", &h.w.A1, "").Want(t, 403)
}

func TestTheOtherSidesRoomsComeBackWithSuggestedMatches(t *testing.T) {
	h := newHotel(t, 1)
	h.connect(t, "channel_manager", "fake", false)
	h.fake.Remotes = []channels.RemoteRate{{RoomID: "RT9", RoomName: "Deluxe Double", RateID: "RP9", RateName: "Flexible rate", Occupancy: 2}, {RoomID: "RT8", RoomName: "Penthouse", RateID: "RP8", RateName: "Best"}}
	got := h.manager.Do("GET", "/api/channel/remote", &h.w.A1, "").Want(t, 200, "Penthouse").Body
	var out struct {
		Remote      []channels.RemoteRate
		Suggestions []struct{ PlanID, RoomID, RateID string }
	}
	testkit.Must(t, json.Unmarshal([]byte(got), &out))
	if len(out.Suggestions) != 1 || out.Suggestions[0].PlanID != h.rp || out.Suggestions[0].RoomID != "RT9" || out.Suggestions[0].RateID != "RP9" {
		t.Fatalf("%s", got)
	}
}

func TestBookingsAreAcknowledgedOnlyOnceKeptAndProblemsGoToStaff(t *testing.T) {
	h := newHotel(t, 1)
	h.connect(t, "channel_manager", "fake", true)
	h.fake.Inbox = []channels.Booking{
		{ExternalID: "A", AckID: "rev-A", Status: "new", GuestName: "One", Arrival: "2026-12-10", Departure: "2026-12-12", ExternalRoomID: "R1", ExternalRateID: "P1", Adults: 2},
		{ExternalID: "B", AckID: "rev-B", Status: "new", GuestName: "Two", Arrival: "2026-12-10", Departure: "2026-12-12", ExternalRoomID: "R1", ExternalRateID: "P1", Adults: 2}, // sold out
		{ExternalID: "C", AckID: "rev-C", Status: "new", GuestName: "Three", Arrival: "2026-12-20", Departure: "2026-12-21", Problem: "the booking has 2 rooms"},
	}
	h.manager.Do("POST", "/api/channel/sync", &h.w.A1, "").Want(t, 200, `"pulled":1`)
	if len(h.fake.Acked) != 1 || h.fake.Acked[0] != "rev-A" {
		t.Fatalf("acked %v: a booking that was not kept must come back", h.fake.Acked)
	}
	if log := h.manager.Do("GET", "/api/channel/log", &h.w.A1, "").Want(t, 200).Body; !strings.Contains(log, "2 rooms") {
		t.Fatalf("the problem is not in the log: %s", log)
	}
}

func TestAModifiedBookingMovesTheStay(t *testing.T) {
	h := newHotel(t, 1)
	h.connect(t, "channel_manager", "fake", true)
	b := channels.Booking{ExternalID: "M1", Status: "new", GuestName: "Kim", Arrival: "2026-12-10", Departure: "2026-12-12", ExternalRoomID: "R1", ExternalRateID: "P1", Adults: 2}
	h.fake.Inbox = []channels.Booking{b}
	h.manager.Do("POST", "/api/channel/sync", &h.w.A1, "").Want(t, 200, `"pulled":1`)
	b.Status, b.Arrival, b.Departure = "modified", "2026-12-14", "2026-12-16"
	h.fake.Inbox = []channels.Booking{b}
	h.manager.Do("POST", "/api/channel/sync", &h.w.A1, "").Want(t, 200, `"pulled":1`)
	if got := h.row(t, `select sold::text from inventory where date = '2026-12-10'`); got != "0" {
		t.Fatalf("the old nights are still held: %s", got)
	}
	if got := h.row(t, `select sold::text from inventory where date = '2026-12-14'`); got != "1" {
		t.Fatalf("the new nights are not held: %s", got)
	}
	h.manager.Do("POST", "/api/channel/sync", &h.w.A1, "").Want(t, 200, `"pulled":0`) // the same change again changes nothing
}

func TestTheNightlyCheckMovesAStayAndImportsABookingThatNoMessageBrought(t *testing.T) {
	h := newHotel(t, 2)
	h.connect(t, "channel_manager", "fake", true)
	b := channels.Booking{ExternalID: "N1", Status: "new", GuestName: "Kim", Arrival: "2026-12-10", Departure: "2026-12-12", ExternalRoomID: "R1", ExternalRateID: "P1", Adults: 2}
	h.fake.Inbox = []channels.Booking{b}
	h.manager.Do("POST", "/api/channel/sync", &h.w.A1, "").Want(t, 200, `"pulled":1`)
	// the other side moved the stay and took a new booking, and neither message reached us
	b.Arrival, b.Departure = "2026-12-14", "2026-12-16"
	m := channels.Booking{ExternalID: "N2", Status: "new", GuestName: "Lee", Arrival: "2026-12-20", Departure: "2026-12-21", ExternalRoomID: "R1", ExternalRateID: "P1", Adults: 1}
	h.fake.Inbox = []channels.Booking{b, m}
	n, err := channels.RunNightly(testkit.Ctx, h.w.Pool, channels.Fixed(h.fake), h.w.Now)
	testkit.Must(t, err)
	if n != 1 {
		t.Fatalf("%d connections checked", n)
	}
	for date, want := range map[string]string{"2026-12-10": "0", "2026-12-14": "1", "2026-12-20": "1"} {
		if got := h.row(t, `select sold::text from inventory where date = $1`, date); got != want {
			t.Errorf("%s sold %s, want %s", date, got, want)
		}
	}
	if got := h.row(t, `select count(*)::text from reservation where source_channel = 'fake' and status <> 'cancelled'`); got != "2" {
		t.Fatalf("%s reservations", got)
	}
}

func TestTheCatalogNamesMarketsAndSaysWhichVendorsNeedTheirApproval(t *testing.T) {
	h := newHotel(t, 1)
	got := h.manager.Do("GET", "/api/channel/providers", &h.w.A1, "").Want(t, 200).Body
	var ps []struct {
		ID          string
		Mode        string
		Access      string
		Connectable bool
		Markets     []string
		Steps       []string
	}
	testkit.Must(t, json.Unmarshal([]byte(got), &ps))
	by := map[string]int{}
	for i, p := range ps {
		by[p.ID] = i
	}
	for id, market := range map[string]string{"channex": "ALL", "beds24": "ALL", "siteminder": "TW", "staah": "VN", "ezee": "TH", "rategain": "SG", "cloudbeds": "US", "omnibees": "MX", "tllincoln": "JP", "neppan": "JP", "mirai": "EU"} {
		i, ok := by[id]
		if !ok {
			t.Errorf("%s is missing", id)
			continue
		}
		p := ps[i]
		if p.Mode != "channel_manager" && id != "agoda" {
			t.Errorf("%s mode %s", id, p.Mode)
		}
		found := false
		for _, m := range p.Markets {
			found = found || m == market
		}
		if !found {
			t.Errorf("%s does not name %s: %v", id, market, p.Markets)
		}
		open := id == "channex" || id == "beds24"
		if p.Connectable != open || (p.Access == "self_serve") != open {
			t.Errorf("%s connectable %v access %s", id, p.Connectable, p.Access)
		}
		if len(p.Steps) == 0 {
			t.Errorf("%s has no steps", id)
		}
	}
	h.manager.Do("PUT", "/api/channel", &h.w.A1, `{"mode":"channel_manager","provider":"beds24","externalHotelId":"77","secretSetting":"BEDS24_TOKEN","enabled":false}`).Want(t, 200)
	h.manager.Do("PUT", "/api/channel", &h.w.A1, `{"mode":"channel_manager","provider":"siteminder","externalHotelId":"77"}`).Want(t, 400)
}
