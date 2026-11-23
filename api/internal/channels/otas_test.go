package channels

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type recorder struct {
	paths  []string
	bodies []string
	auth   []string
	query  []string
	reply  map[string]string
	srv    *httptest.Server
}

func newRecorder(t *testing.T) *recorder {
	r := &recorder{reply: map[string]string{}}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		r.paths = append(r.paths, req.Method+" "+req.URL.Path)
		r.bodies = append(r.bodies, string(b))
		r.auth = append(r.auth, req.Header.Get("Authorization"))
		r.query = append(r.query, req.URL.RawQuery)
		if body, ok := r.reply[req.URL.Path]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		_, _ = w.Write([]byte(`<OK><Success/></OK>`))
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *recorder) body(path string) string {
	for i, p := range r.paths {
		if strings.HasSuffix(p, path) {
			return r.bodies[i]
		}
	}
	return ""
}

func contains(t *testing.T, what, body string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("%s lacks %s:\n%s", what, w, body)
		}
	}
}

var oneNight = []Slot{
	{ExternalRoomID: "R1", ExternalRateID: "P1", Date: "2026-12-10", Available: 2, Price: 12345, Currency: "USD"},
	{ExternalRoomID: "R1", ExternalRateID: "P1", Date: "2026-12-11", Available: 0, Price: 12345, Currency: "USD"},
}

func TestGooglePushesInventoryRatesAndOpensOrClosesSales(t *testing.T) {
	r := newRecorder(t)
	g := NewGoogle(r.srv.URL, "HOTEL1", "PARTNER", r.srv.Client())
	if err := g.PushAvailability(context.Background(), oneNight); err != nil {
		t.Fatal(err)
	}
	contains(t, "inventory", r.body("/travel/hotels/uploads/ota/hotel_inv_count_notif"), "OTA_HotelInvCountNotifRQ", `HotelCode="HOTEL1"`, `InvTypeCode="R1"`, `Count="2"`, `Count="0"`, `RequestorID ID="PARTNER"`)
	contains(t, "rates", r.body("/travel/hotels/uploads/ota/hotel_rate_amount_notif"), "OTA_HotelRateAmountNotifRQ", `RatePlanCode="P1"`, `AmountAfterTax="123.45"`, `CurrencyCode="USD"`, `Start="2026-12-10"`)
	avail := r.body("/travel/hotels/uploads/ota/hotel_avail_notif")
	contains(t, "availability", avail, "OTA_HotelAvailNotifRQ", `Status="Open"`, `Status="Close"`, `Restriction="Master"`)
	if strings.Count(r.body("/travel/hotels/uploads/ota/hotel_inv_count_notif"), "<Inventory>") != 2 {
		t.Error("inventory goes once per room and night")
	}
	if got, err := g.PullBookings(context.Background(), time.Now()); err != nil || len(got) != 0 {
		t.Fatalf("Google sends guests to your site and returns no bookings: %v %v", got, err)
	}
}

func TestGoogleErrorsAreReported(t *testing.T) {
	r := newRecorder(t)
	r.reply["/travel/hotels/uploads/ota/hotel_inv_count_notif"] = `<OTA_HotelInvCountNotifRS><Errors><Error Type="12" Code="450" ShortText="8001 unknown hotel"/></Errors></OTA_HotelInvCountNotifRS>`
	if err := NewGoogle(r.srv.URL, "H", "", r.srv.Client()).PushAvailability(context.Background(), oneNight); err == nil || !strings.Contains(err.Error(), "unknown hotel") {
		t.Fatalf("%v", err)
	}
}

func TestBookingComUsesItsOwnPathsAndBasicAuthAndReadsReservations(t *testing.T) {
	r := newRecorder(t)
	r.reply["/hotels/ota/OTA_HotelResNotif"] = `<OTA_HotelResNotifRQ><HotelReservations><HotelReservation>
<RoomStays><RoomStay><RoomTypes><RoomType RoomTypeCode="R1"/></RoomTypes><RatePlans><RatePlan RatePlanCode="P1"/></RatePlans>
<GuestCounts><GuestCount AgeQualifyingCode="10" Count="2"/><GuestCount AgeQualifyingCode="8" Count="1"/></GuestCounts><TimeSpan Start="2026-12-10" End="2026-12-12"/></RoomStay></RoomStays>
<ResGuests><ResGuest><Profiles><ProfileInfo><Profile><Customer><PersonName><GivenName>Kim</GivenName><Surname>Park</Surname></PersonName></Customer></Profile></ProfileInfo></Profiles></ResGuest></ResGuests>
<ResGlobalInfo><HotelReservationIDs><HotelReservationID ResID_Value="BK-1" ResID_Type="14"/></HotelReservationIDs><Profiles><ProfileInfo><Profile><Customer><Email>kim@x.com</Email></Customer></Profile></ProfileInfo></Profiles></ResGlobalInfo>
</HotelReservation></HotelReservations></OTA_HotelResNotifRQ>`
	r.reply["/hotels/ota/OTA_HotelResModifyNotif"] = `<OTA_HotelResModifyNotifRQ><HotelReservations><HotelReservation ResStatus="Cancel"><RoomStays><RoomStay><RoomTypes><RoomType RoomTypeCode="R1"/></RoomTypes><TimeSpan Start="2026-12-20" End="2026-12-21"/></RoomStay></RoomStays>
<ResGlobalInfo><HotelReservationIDs><HotelReservationID ResID_Value="BK-2" ResID_Type="18"/></HotelReservationIDs></ResGlobalInfo></HotelReservation></HotelReservations></OTA_HotelResModifyNotifRQ>`
	b := NewBookingCom(r.srv.URL, "H1", "machine:pw", r.srv.Client())
	if err := b.PushAvailability(context.Background(), oneNight); err != nil {
		t.Fatal(err)
	}
	contains(t, "availability", r.body("/hotels/ota/OTA_HotelAvailNotif"), `HotelCode="H1"`, `BookingLimit="2"`, `InvTypeCode="R1"`, `RatePlanCode="P1"`)
	contains(t, "rates", r.body("/hotels/ota/OTA_HotelRateAmountNotif"), `AmountAfterTax="123.45"`)
	if !strings.HasPrefix(r.auth[0], "Basic ") {
		t.Fatalf("%q", r.auth[0])
	}
	got, err := b.PullBookings(context.Background(), time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if x := got[0]; x.ExternalID != "BK-1" || x.Status != "new" || x.GuestName != "Kim Park" || x.Email != "kim@x.com" || x.Adults != 2 || x.Children != 1 || x.ExternalRoomID != "R1" || x.ExternalRateID != "P1" || x.Arrival != "2026-12-10" || x.Departure != "2026-12-12" || x.AckID != "new:BK-1" {
		t.Fatalf("%+v", x)
	}
	if got[1].Status != "cancelled" || got[1].ExternalID != "BK-2" || got[1].AckID != "mod:BK-2" {
		t.Fatalf("%+v", got[1])
	}
	if !strings.Contains(strings.Join(r.query, " "), "hotel_ids=H1") || !strings.Contains(strings.Join(r.query, " "), "last_change=") {
		t.Fatalf("%v", r.query)
	}
	if err := b.(Acker).Ack(context.Background(), []string{"new:BK-1", "mod:BK-2"}); err != nil {
		t.Fatal(err)
	}
	contains(t, "new ack", r.body("POST /hotels/ota/OTA_HotelResNotif"+"")+r.joined("OTA_HotelResNotif"), "BK-1")
	contains(t, "modified ack", r.joined("OTA_HotelResModifyNotif"), "BK-2")
}

func (r *recorder) joined(path string) string {
	var out string
	for i, p := range r.paths {
		if strings.HasPrefix(p, "POST") && strings.HasSuffix(p, path) {
			out += r.bodies[i]
		}
	}
	return out
}

func TestExpediaPushesOneAvailRateUpdatePerNightWithTheLoginInTheBody(t *testing.T) {
	r := newRecorder(t)
	r.reply["/eqc/ar"] = `<AvailRateUpdateRS><Success/></AvailRateUpdateRS>`
	e := NewExpedia(r.srv.URL, "12345", "eqcuser:eqcpass", r.srv.Client())
	if err := e.PushAvailability(context.Background(), oneNight); err != nil {
		t.Fatal(err)
	}
	body := r.body("/eqc/ar")
	contains(t, "avail rate update", body, "AvailRateUpdateRQ", `username="eqcuser"`, `password="eqcpass"`, `<Hotel id="12345"`, `from="2026-12-10"`, `to="2026-12-10"`, `<RoomType id="R1"`, `totalInventoryAvailable="2"`, `totalInventoryAvailable="0"`, `<RatePlan id="P1"`, `currency="USD"`, `rate="123.45"`)
	r.reply["/eqc/ar"] = `<AvailRateUpdateRS><Error code="3203" message="Rate plan is not active"/></AvailRateUpdateRS>`
	if err := e.PushAvailability(context.Background(), oneNight[:1]); err == nil || !strings.Contains(err.Error(), "Rate plan is not active") {
		t.Fatalf("%v", err)
	}
}

func TestExpediaBookingsAreRetrievedAndConfirmedWhenAsked(t *testing.T) {
	r := newRecorder(t)
	r.reply["/eqc/br"] = `<BookingRetrievalRS><Bookings>
<Booking id="EX-1" type="Book"><RoomStay roomTypeID="R1" ratePlanID="P1"><StayDate arrival="2026-12-10" departure="2026-12-12"/><GuestCount adult="2" child="1"/></RoomStay><PrimaryGuest><Name givenName="Kim" surname="Park"/><Email>kim@x.com</Email></PrimaryGuest></Booking>
<Booking id="EX-2" type="Cancel"><RoomStay roomTypeID="R1" ratePlanID="P1"><StayDate arrival="2026-12-20" departure="2026-12-21"/></RoomStay></Booking>
<Booking id="EX-3" type="Modify"><RoomStay roomTypeID="R1" ratePlanID="P1"><StayDate arrival="2026-12-22" departure="2026-12-24"/></RoomStay></Booking>
</Bookings></BookingRetrievalRS>`
	e := NewExpedia(r.srv.URL, "12345", "u:p", r.srv.Client())
	got, err := e.PullBookings(context.Background(), time.Now())
	if err != nil || len(got) != 3 {
		t.Fatalf("%+v %v", got, err)
	}
	if x := got[0]; x.ExternalID != "EX-1" || x.Status != "new" || x.GuestName != "Kim Park" || x.Email != "kim@x.com" || x.Adults != 2 || x.Children != 1 || x.ExternalRoomID != "R1" || x.ExternalRateID != "P1" || x.Arrival != "2026-12-10" || x.AckID != "EX-1|Book" {
		t.Fatalf("%+v", x)
	}
	if got[1].Status != "cancelled" || got[2].Status != "modified" {
		t.Fatalf("%+v %+v", got[1], got[2])
	}
	if r.body("/eqc/bc") != "" {
		t.Fatal("pulling must not confirm")
	}
	if err := e.(Acker).Ack(context.Background(), []string{"EX-1|Book"}); err != nil {
		t.Fatal(err)
	}
	contains(t, "confirm", r.body("/eqc/bc"), "BookingConfirmRQ", `bookingID="EX-1"`, `type="Book"`, `username="u"`)
}
