package channels_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/channels"
	"hotelpms/api/internal/folio"
	"hotelpms/api/internal/rooms"
	"hotelpms/api/internal/testkit"
)

// mock stands in for a booking site's servers. It records what was sent to it and answers each path with a canned reply.
type mock struct {
	mu      sync.Mutex
	replies map[string]string
	calls   []string // method, path and body of every request
	srv     *httptest.Server
}

func newMock(t *testing.T, replies map[string]string) *mock {
	m := &mock{replies: replies}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		m.calls = append(m.calls, r.Method+" "+r.URL.Path+" "+string(b))
		m.mu.Unlock()
		if body, ok := m.replies[r.URL.Path]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		_, _ = w.Write([]byte(`<OK><Success/></OK>`))
	}))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mock) saw(substr string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.calls {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

// newHotelFor builds a hotel of two rooms whose channel connection talks to the given adapter, as the real one would.
func newHotelFor(t *testing.T, ad channels.Adapter) *hotel {
	t.Helper()
	w := testkit.New(t, rooms.Mount, booking.Mount, folio.Mount, channels.Mount(channels.Fixed(ad)))
	h := &hotel{w: w}
	h.owner = w.Member(w.OrgA, nil, "owner")
	h.manager = w.Member(w.OrgA, &w.A1, "manager")
	h.desk = w.Member(w.OrgA, &w.A1, "front_desk")
	h.rt = idOf(t, h.manager.Do("POST", "/api/room-types", &w.A1, `{"name":"Deluxe","baseGuests":2,"maxGuests":3}`).Want(t, 201))
	for i := 0; i < 2; i++ {
		h.manager.Do("POST", "/api/rooms", &w.A1, fmt.Sprintf(`{"roomTypeId":%q,"number":"%d","floor":1}`, h.rt, 101+i)).Want(t, 201)
	}
	h.rp = idOf(t, h.manager.Do("POST", "/api/rate-plans", &w.A1, fmt.Sprintf(`{"roomTypeId":%q,"name":"Flexible"}`, h.rt)).Want(t, 201))
	h.manager.Do("PUT", "/api/rates", &w.A1, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-11-23","to":"2027-01-01","price":10000}`, h.rp)).Want(t, 200)
	return h
}

// connectTo saves the connection and matches the hotel's rate plan to the site's room and rate codes.
func (h *hotel) connectTo(t *testing.T, mode, provider, room, rate string) {
	t.Helper()
	h.manager.Do("PUT", "/api/channel", &h.w.A1, fmt.Sprintf(`{"mode":%q,"provider":%q,"externalHotelId":"H1","secretSetting":"KEY","enabled":true}`, mode, provider)).Want(t, 200)
	h.manager.Do("PUT", "/api/channel/map", &h.w.A1, fmt.Sprintf(`{"ratePlanId":%q,"externalRoomId":%q,"externalRateId":%q}`, h.rp, room, rate)).Want(t, 200)
}

func (h *hotel) soldOn(t *testing.T, date string) string {
	return h.row(t, `select sold::text from inventory where date = $1`, date)
}

func TestBeds24BookingComAndExpediaEachBringABookingInAndGetTheNightsPushedBack(t *testing.T) {
	cases := []struct {
		name, mode, provider, room, rate, source string
		replies                                  map[string]string
		build                                    func(base string) channels.Adapter
		pushed                                   string
	}{
		{"Beds24", "channel_manager", "beds24", "1", "", "beds24",
			map[string]string{"/bookings": `{"success":true,"data":[{"id":501,"roomId":1,"status":"confirmed","arrival":"2026-12-10","departure":"2026-12-12","numAdult":2,"firstName":"Kim","lastName":"Park","email":"kim@x.com","channel":"booking"}]}`, "/inventory/rooms/calendar": `{"success":true}`},
			func(b string) channels.Adapter { return channels.NewBeds24(b, "H1", "tok", http.DefaultClient) }, "/inventory/rooms/calendar"},
		{"Booking.com", "direct", "booking_com", "R1", "P1", "booking_com",
			map[string]string{"/hotels/ota/OTA_HotelResNotif": `<OTA_HotelResNotifRQ><HotelReservations><HotelReservation>
<RoomStays><RoomStay><RoomTypes><RoomType RoomTypeCode="R1"/></RoomTypes><RatePlans><RatePlan RatePlanCode="P1"/></RatePlans><GuestCounts><GuestCount AgeQualifyingCode="10" Count="2"/></GuestCounts><TimeSpan Start="2026-12-10" End="2026-12-12"/></RoomStay></RoomStays>
<ResGuests><ResGuest><Profiles><ProfileInfo><Profile><Customer><PersonName><GivenName>Kim</GivenName><Surname>Park</Surname></PersonName></Customer></Profile></ProfileInfo></Profiles></ResGuest></ResGuests>
<ResGlobalInfo><HotelReservationIDs><HotelReservationID ResID_Value="BK-1" ResID_Type="14"/></HotelReservationIDs></ResGlobalInfo>
</HotelReservation></HotelReservations></OTA_HotelResNotifRQ>`},
			func(b string) channels.Adapter { return channels.NewBookingCom(b, "H1", "user:pw", http.DefaultClient) }, "OTA_HotelAvailNotif"},
		{"Expedia", "direct", "expedia", "R1", "P1", "expedia",
			map[string]string{"/eqc/br": `<BookingRetrievalRS><Bookings><Booking id="EX-1" type="Book"><RoomStay roomTypeID="R1" ratePlanID="P1"><StayDate arrival="2026-12-10" departure="2026-12-12"/><GuestCount adult="2" child="0"/></RoomStay><PrimaryGuest><Name givenName="Kim" surname="Park"/><Email>kim@x.com</Email></PrimaryGuest></Booking></Bookings></BookingRetrievalRS>`, "/eqc/ar": `<AvailRateUpdateRS><Success/></AvailRateUpdateRS>`},
			func(b string) channels.Adapter { return channels.NewExpedia(b, "H1", "u:p", http.DefaultClient) }, "AvailRateUpdateRQ"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newMock(t, c.replies)
			h := newHotelFor(t, c.build(m.srv.URL))
			h.connectTo(t, c.mode, c.provider, c.room, c.rate)
			h.manager.Do("POST", "/api/channel/sync", &h.w.A1, "").Want(t, 200, `"pulled":1`)
			if got := h.row(t, `select count(*)::text from reservation where source_channel = $1 and status <> 'cancelled'`, c.source); got != "1" {
				t.Fatalf("%s reservations from %s", got, c.source)
			}
			if got := h.soldOn(t, "2026-12-10"); got != "1" {
				t.Fatalf("the booking did not take its room: sold %s", got)
			}
			if !m.saw(c.pushed) {
				t.Fatalf("nothing was pushed to %s (calls: %v)", c.pushed, m.calls)
			}
			// What went out already shows the room taken, and a second sync does not book it again.
			h.manager.Do("POST", "/api/channel/sync", &h.w.A1, "").Want(t, 200)
			if got := h.row(t, `select count(*)::text from reservation where source_channel = $1 and status <> 'cancelled'`, c.source); got != "1" {
				t.Fatalf("a second sync made %s reservations", got)
			}
			h.manager.Do("GET", "/api/channel/log", &h.w.A1, "").Want(t, 200, `"ok"`)
		})
	}
}

func TestASiteThatAnswersWithAnErrorIsLoggedAndDoesNotStopTheHotel(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", 503) }))
	defer bad.Close()
	h := newHotelFor(t, channels.NewBookingCom(bad.URL, "H1", "user:pw", http.DefaultClient))
	h.connectTo(t, "direct", "booking_com", "R1", "P1")
	h.manager.Do("POST", "/api/channel/sync", &h.w.A1, "").Want(t, 200)
	h.manager.Do("GET", "/api/channel/log", &h.w.A1, "").Want(t, 200, `"error"`)
	h.manager.Do("POST", "/api/reservations", &h.w.A1, fmt.Sprintf(`{"guest":{"name":"Mai"},"rooms":[{"roomTypeId":%q,"ratePlanId":%q,"from":"2026-12-10","to":"2026-12-11","adults":2,"children":0}]}`, h.rt, h.rp)).Want(t, 201)
}
