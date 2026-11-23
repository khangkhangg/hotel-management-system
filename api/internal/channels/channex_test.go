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

type channexServer struct {
	t        *testing.T
	avail    []map[string]any
	rest     []map[string]any
	acked    []string
	warnings string
	feed     string
	srv      *httptest.Server
}

func newChannex(t *testing.T) *channexServer {
	c := &channexServer{t: t, warnings: `[]`}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("user-api-key") != "key1" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"errors":{"code":"unauthorized","title":"Unauthorized"}}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		decode := func() []map[string]any {
			var in struct {
				Values []map[string]any `json:"values"`
			}
			_ = json.Unmarshal(body, &in)
			return in.Values
		}
		switch {
		case r.URL.Path == "/api/v1/availability" && r.Method == "POST":
			c.avail = append(c.avail, decode()...)
			_, _ = w.Write([]byte(`{"data":[{"id":"t1","type":"task"}],"meta":{"message":"Success","warnings":` + c.warnings + `}}`))
		case r.URL.Path == "/api/v1/restrictions" && r.Method == "POST":
			c.rest = append(c.rest, decode()...)
			_, _ = w.Write([]byte(`{"data":[{"id":"t2","type":"task"}],"meta":{"message":"Success","warnings":` + c.warnings + `}}`))
		case r.URL.Path == "/api/v1/booking_revisions/feed":
			if r.URL.Query().Get("filter[property_id]") != "PROP" {
				http.Error(w, "no property", 400)
				return
			}
			_, _ = w.Write([]byte(c.feed))
		case strings.HasSuffix(r.URL.Path, "/ack") && r.Method == "POST":
			parts := strings.Split(r.URL.Path, "/")
			c.acked = append(c.acked, parts[len(parts)-2])
			_, _ = w.Write([]byte(`{"meta":{"message":"Success"}}`))
		case r.URL.Path == "/api/v1/properties/PROP":
			_, _ = w.Write([]byte(`{"data":{"id":"PROP","type":"property","attributes":{"title":"Sea View","currency":"USD","timezone":"Asia/Ho_Chi_Minh"}}}`))
		case r.URL.Path == "/api/v1/room_types/options":
			_, _ = w.Write([]byte(`{"data":[{"id":"RT1","type":"room_type","attributes":{"title":"Deluxe Double","default_occupancy":2}}]}`))
		case r.URL.Path == "/api/v1/rate_plans/options":
			_, _ = w.Write([]byte(`{"data":[{"id":"RP1","type":"rate_plan","attributes":{"title":"Flexible rate","occupancy":2,"room_type_id":"RT1"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *channexServer) adapter(key string) *Channex {
	return NewChannex(c.srv.URL+"/api/v1", "PROP", key, c.srv.Client())
}

func TestChannexPushesAvailabilityPerRoomTypeAndRatesPerRatePlan(t *testing.T) {
	c := newChannex(t)
	slots := []Slot{
		{ExternalRoomID: "RT1", ExternalRateID: "RP1", Date: "2026-12-10", Available: 2, Price: 10000, Currency: "USD"},
		{ExternalRoomID: "RT1", ExternalRateID: "RP2", Date: "2026-12-10", Available: 2, Price: 12000, Currency: "USD"}, // same room type, second plan
	}
	if err := c.adapter("key1").PushAvailability(context.Background(), slots); err != nil {
		t.Fatal(err)
	}
	if len(c.avail) != 1 || c.avail[0]["room_type_id"] != "RT1" || c.avail[0]["availability"] != float64(2) || c.avail[0]["property_id"] != "PROP" || c.avail[0]["date"] != "2026-12-10" {
		t.Fatalf("availability goes once per room type: %v", c.avail)
	}
	if len(c.rest) != 2 || c.rest[0]["rate_plan_id"] != "RP1" || c.rest[0]["rate"] != float64(10000) || c.rest[1]["rate"] != float64(12000) {
		t.Fatalf("%v", c.rest)
	}
}

func TestChannexWarningsAndBadKeysAreErrors(t *testing.T) {
	c := newChannex(t)
	c.warnings = `[{"warning":"rate must be positive"}]`
	if err := c.adapter("key1").PushAvailability(context.Background(), []Slot{{ExternalRoomID: "RT1", ExternalRateID: "RP1", Date: "2026-12-10", Available: 1, Price: 100}}); err == nil || !strings.Contains(err.Error(), "rate must be positive") {
		t.Fatalf("a 200 with warnings was taken as success: %v", err)
	}
	if err := c.adapter("wrong").PushAvailability(context.Background(), []Slot{{Date: "2026-12-10"}}); err == nil {
		t.Fatal("a bad key was taken as success")
	}
}

func TestChannexBookingsComeFromTheFeedWithNamesAndAreAckedOnlyWhenAsked(t *testing.T) {
	c := newChannex(t)
	c.feed = `{"data":[
{"id":"REV1","type":"booking_revision","attributes":{"booking_id":"BK1","status":"new","ota_name":"Booking.com","arrival_date":"2026-12-10","departure_date":"2026-12-12","occupancy":{"adults":2,"children":1,"infants":0},
 "customer":{"name":"Kim","surname":"Park","mail":"kim@x.com"},"rooms":[{"room_type_id":"RT1","rate_plan_id":"RP1","occupancy":{"adults":2,"children":1}}]}},
{"id":"REV2","type":"booking_revision","attributes":{"booking_id":"BK2","status":"cancelled","arrival_date":"2026-12-20","departure_date":"2026-12-21","customer":{"name":"Li","surname":"Wu","mail":"***"},"rooms":[{"room_type_id":"RT1","rate_plan_id":"RP1"}]}},
{"id":"REV3","type":"booking_revision","attributes":{"booking_id":"BK3","status":"new","arrival_date":"2026-12-22","departure_date":"2026-12-23","customer":{"name":"Two","surname":"Rooms"},"rooms":[{"room_type_id":"RT1","rate_plan_id":"RP1"},{"room_type_id":"RT1","rate_plan_id":"RP1"}]}}
]}`
	a := c.adapter("key1")
	got, err := a.PullBookings(context.Background(), time.Now())
	if err != nil || len(got) != 3 {
		t.Fatalf("%+v %v", got, err)
	}
	if b := got[0]; b.ExternalID != "BK1" || b.AckID != "REV1" || b.Status != "new" || b.GuestName != "Kim Park" || b.Email != "kim@x.com" || b.Adults != 2 || b.Children != 1 || b.ExternalRoomID != "RT1" || b.ExternalRateID != "RP1" || b.Source != "Booking.com" {
		t.Fatalf("%+v", b)
	}
	if got[1].Status != "cancelled" || got[1].Email != "" {
		t.Fatalf("a masked address must not be kept: %+v", got[1])
	}
	if got[2].Problem == "" {
		t.Fatalf("a booking with two rooms must be flagged for staff: %+v", got[2])
	}
	if len(c.acked) != 0 {
		t.Fatalf("pulling must not ack: %v", c.acked)
	}
	testAck(t, a, "REV1")
	if len(c.acked) != 1 || c.acked[0] != "REV1" {
		t.Fatalf("%v", c.acked)
	}
}

func testAck(t *testing.T, a Adapter, id string) {
	t.Helper()
	ak, ok := a.(Acker)
	if !ok {
		t.Fatal("the adapter cannot acknowledge")
	}
	if err := ak.Ack(context.Background(), []string{id}); err != nil {
		t.Fatal(err)
	}
}

func TestChannexTestsTheKeyAndListsWhatToMap(t *testing.T) {
	c := newChannex(t)
	a := c.adapter("key1")
	if err := a.Test(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.adapter("wrong").Test(context.Background()); err == nil {
		t.Fatal("a bad key passed the test")
	}
	rem, err := a.Remote(context.Background())
	if err != nil || len(rem) != 1 || rem[0].RoomID != "RT1" || rem[0].RateID != "RP1" || rem[0].RoomName != "Deluxe Double" || rem[0].RateName != "Flexible rate" || rem[0].Occupancy != 2 {
		t.Fatalf("%+v %v", rem, err)
	}
}
