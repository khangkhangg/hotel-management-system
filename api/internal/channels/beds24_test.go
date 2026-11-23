package channels

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestBeds24PushesAvailabilityAndPricePerRoomAndNightWithTheTokenHeader(t *testing.T) {
	r := newRecorder(t)
	r.reply["/inventory/rooms/calendar"] = `{"success":true}`
	a := NewBeds24(r.srv.URL, "77", "tok3n", r.srv.Client())
	nights := []Slot{{ExternalRoomID: "1", Date: "2026-12-10", Available: 2, Price: 12345, Currency: "USD"}, {ExternalRoomID: "1", Date: "2026-12-11", Available: 0, Price: 12345, Currency: "USD"}}
	if err := a.PushAvailability(context.Background(), nights); err != nil {
		t.Fatal(err)
	}
	body := r.body("/inventory/rooms/calendar")
	contains(t, "calendar", body, `"roomId":1`, `"from":"2026-12-10"`, `"to":"2026-12-10"`, `"numAvail":2`, `"numAvail":0`, `"price1":123.45`)
	r.reply["/inventory/rooms/calendar"] = `{"success":false,"error":"room not found","code":404}`
	if err := a.PushAvailability(context.Background(), nights[:1]); err == nil || !strings.Contains(err.Error(), "room not found") {
		t.Fatalf("%v", err)
	}
}

func TestBeds24BookingsComeBackAsChangesBecauseItNeverSaysModified(t *testing.T) {
	r := newRecorder(t)
	r.reply["/bookings"] = `{"success":true,"data":[
{"id":501,"roomId":1,"status":"confirmed","arrival":"2026-12-10","departure":"2026-12-12","numAdult":2,"numChild":1,"firstName":"Kim","lastName":"Park","email":"kim@x.com","channel":"booking"},
{"id":502,"roomId":1,"status":"cancelled","arrival":"2026-12-20","departure":"2026-12-21","numAdult":1,"firstName":"Lee","lastName":"Cho"},
{"id":503,"roomId":1,"status":"request","arrival":"2026-12-22","departure":"2026-12-23","numAdult":1},
{"id":504,"roomId":1,"status":"black","arrival":"2026-12-24","departure":"2026-12-25"}]}`
	a := NewBeds24(r.srv.URL, "77", "tok3n", r.srv.Client())
	got, err := a.PullBookings(context.Background(), time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if x := got[0]; x.ExternalID != "501" || x.Status != "modified" || x.GuestName != "Kim Park" || x.Email != "kim@x.com" || x.Adults != 2 || x.Children != 1 || x.ExternalRoomID != "1" || x.Arrival != "2026-12-10" || x.Source != "booking" {
		t.Fatalf("%+v", x)
	}
	if got[1].Status != "cancelled" || got[1].ExternalID != "502" {
		t.Fatalf("%+v", got[1])
	}
	q := strings.Join(r.query, " ")
	if !strings.Contains(q, "propertyId=77") || !strings.Contains(q, "modifiedFrom=2026-11-23") {
		t.Fatalf("%v", r.query)
	}
}

func TestBeds24TestsTheTokenAndListsRooms(t *testing.T) {
	var token string
	r := newRecorder(t)
	srv := r.srv
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		token = q.Header.Get("token")
		switch q.URL.Path {
		case "/authentication/details":
			_, _ = w.Write([]byte(`{"validToken":true}`))
		case "/properties":
			_, _ = w.Write([]byte(`{"success":true,"data":[{"id":77,"name":"Sea View","roomTypes":[{"id":1,"name":"Deluxe","qty":3,"maxPeople":2},{"id":2,"name":"Suite","qty":1,"maxPeople":4}]}]}`))
		}
	})
	a := NewBeds24(srv.URL, "77", "tok3n", srv.Client())
	if err := a.(Tester).Test(context.Background()); err != nil || token != "tok3n" {
		t.Fatalf("%v %q", err, token)
	}
	rr, err := a.(Lister).Remote(context.Background())
	if err != nil || len(rr) != 2 || rr[0].RoomID != "1" || rr[0].RoomName != "Deluxe" || rr[0].Occupancy != 2 || rr[1].RoomName != "Suite" {
		t.Fatalf("%+v %v", rr, err)
	}
}
