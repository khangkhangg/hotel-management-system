package channels

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTheChannelManagerAdapterSendsNightsInBatchesWithTheSecretAndReadsBookings(t *testing.T) {
	var calls int
	var auth string
	var lastBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/availability":
			calls++
			lastBody = map[string]any{}
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &lastBody)
		case "/bookings":
			if r.URL.Query().Get("hotelId") != "H1" || r.URL.Query().Get("since") == "" {
				http.Error(w, "bad query", 400)
				return
			}
			_, _ = w.Write([]byte(`{"bookings":[{"id":"X1","status":"new","guestName":"Kim","email":"k@x.com","arrival":"2026-12-10","departure":"2026-12-12","roomId":"R1","rateId":"P1","adults":2,"children":1}]}`))
		}
	}))
	defer srv.Close()
	a := NewChannelManager(srv.URL, "H1", "s3cret", srv.Client())
	slots := make([]Slot, 1200)
	for i := range slots {
		slots[i] = Slot{ExternalRoomID: "R1", Date: "2026-12-10", Available: 1, Price: 10000, Currency: "USD"}
	}
	if err := a.PushAvailability(context.Background(), slots); err != nil {
		t.Fatal(err)
	}
	if calls != 3 || auth != "Bearer s3cret" || lastBody["hotelId"] != "H1" {
		t.Fatalf("calls %d auth %q body %v", calls, auth, lastBody)
	}
	got, err := a.PullBookings(context.Background(), time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC))
	if err != nil || len(got) != 1 || got[0].ExternalID != "X1" || got[0].Children != 1 || got[0].ExternalRoomID != "R1" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestAnErrorFromTheOtherSideIsReportedNotSwallowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "nope", 502) }))
	defer srv.Close()
	if err := NewChannelManager(srv.URL, "H1", "s", srv.Client()).PushAvailability(context.Background(), []Slot{{Date: "2026-12-10"}}); err == nil {
		t.Fatal("a 502 was taken as success")
	}
	if err := NewOpenTravel(srv.URL, "H1", "s", srv.Client()).PushAvailability(context.Background(), []Slot{{Date: "2026-12-10"}}); err == nil {
		t.Fatal("a 502 was taken as success")
	}
}

func TestTheOpenTravelAdapterSendsAvailabilityAndRatesAndReadsReservations(t *testing.T) {
	var bodies []string
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		auth = r.Header.Get("Authorization")
		bodies = append(bodies, string(b))
		if strings.Contains(string(b), "OTA_ReadRQ") {
			_, _ = w.Write([]byte(`<OTA_ResRetrieveRS><ReservationsList>
<HotelReservation ResStatus="Commit"><UniqueID ID="B-77"/><RoomStays><RoomStay><RoomTypes><RoomType RoomTypeCode="R1"/></RoomTypes><RatePlans><RatePlan RatePlanCode="P1"/></RatePlans>
<GuestCounts><GuestCount AgeQualifyingCode="10" Count="2"/><GuestCount AgeQualifyingCode="8" Count="1"/></GuestCounts><TimeSpan Start="2026-12-10" End="2026-12-12"/></RoomStay></RoomStays>
<ResGuests><ResGuest><Profiles><ProfileInfo><Profile><Customer><PersonName><GivenName>Kim</GivenName><Surname>Park</Surname></PersonName><Email>kim@x.com</Email></Customer></Profile></ProfileInfo></Profiles></ResGuest></ResGuests></HotelReservation>
<HotelReservation ResStatus="Cancel"><UniqueID ID="B-78"/><RoomStays><RoomStay><RoomTypes><RoomType RoomTypeCode="R1"/></RoomTypes><TimeSpan Start="2026-12-20" End="2026-12-21"/></RoomStay></RoomStays></HotelReservation>
</ReservationsList></OTA_ResRetrieveRS>`))
		}
	}))
	defer srv.Close()
	a := NewOpenTravel(srv.URL, "H1", "s3cret", srv.Client())
	err := a.PushAvailability(context.Background(), []Slot{{ExternalRoomID: "R1", ExternalRateID: "P1", Date: "2026-12-10", Available: 2, Price: 12345, Currency: "USD"}})
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(bodies, "\n")
	for _, want := range []string{"OTA_HotelAvailNotifRQ", `BookingLimit="2"`, `InvTypeCode="R1"`, `RatePlanCode="P1"`, `Start="2026-12-10"`, "OTA_HotelRateAmountNotifRQ", `AmountAfterTax="123.45"`, `CurrencyCode="USD"`, `HotelCode="H1"`} {
		if !strings.Contains(all, want) {
			t.Errorf("the XML lacks %s:\n%s", want, all)
		}
	}
	if !strings.HasPrefix(auth, "Basic ") {
		t.Fatalf("auth %q", auth)
	}
	got, err := a.PullBookings(context.Background(), time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC))
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if b := got[0]; b.ExternalID != "B-77" || b.Status != "new" || b.GuestName != "Kim Park" || b.Email != "kim@x.com" || b.Adults != 2 || b.Children != 1 || b.Arrival != "2026-12-10" || b.ExternalRateID != "P1" {
		t.Fatalf("%+v", b)
	}
	if got[1].Status != "cancelled" || got[1].ExternalID != "B-78" {
		t.Fatalf("%+v", got[1])
	}
}

func TestTheGuardedClientRefusesPrivateAddresses(t *testing.T) {
	c := GuardedClient()
	for _, u := range []string{"http://127.0.0.1:1/", "http://10.0.0.5/", "http://169.254.169.254/latest/meta-data/", "http://[::1]/"} {
		if resp, err := c.Get(u); err == nil {
			resp.Body.Close()
			t.Errorf("%s was reached", u)
		}
	}
}

func TestTheRealFactoryNeedsTheSecretSettingToBeSet(t *testing.T) {
	f := Real(func(k string) string {
		if k == "AGODA_KEY" {
			return "abc"
		}
		return ""
	})
	if _, err := f(Connection{Provider: "agoda", BaseURL: "https://ota.example.com/x", SecretSetting: "NOPE", ExternalHotelID: "H"}); err == nil {
		t.Fatal("an unset secret was accepted")
	}
	if _, err := f(Connection{Provider: "agoda", BaseURL: "", SecretSetting: "AGODA_KEY"}); err == nil {
		t.Fatal("no address was accepted")
	}
	if a, err := f(Connection{Provider: "agoda", BaseURL: "https://ota.example.com/x", SecretSetting: "AGODA_KEY", ExternalHotelID: "H"}); err != nil || a == nil {
		t.Fatalf("%v", err)
	}
	if _, err := f(Connection{Provider: "cm_generic", BaseURL: "https://cm.example.com", SecretSetting: "AGODA_KEY", ExternalHotelID: "H"}); err != nil {
		t.Fatalf("%v", err)
	}
	if _, err := f(Connection{Provider: "mystery"}); err == nil {
		t.Fatal("unknown provider")
	}
}
