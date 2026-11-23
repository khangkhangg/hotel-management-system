package channels

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

var typeRE = regexp.MustCompile(`type="(\d+)"`)

type agodaServer struct {
	tokens   int
	bodies   map[string][]string
	status   int
	listBody string
	srv      *httptest.Server
}

func newAgoda(t *testing.T) *agodaServer {
	a := &agodaServer{bodies: map[string][]string{}, status: 200}
	a.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/token-based-authentication/v2/exchange" {
			a.tokens++
			if !strings.Contains(string(b), "client_id=cid") || !strings.Contains(string(b), "client_secret=csecret") || !strings.Contains(string(b), "grant_type=client_credentials") {
				http.Error(w, "bad", 400)
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"tok1","token_type":"Bearer","expires_in":3600}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok1" {
			w.WriteHeader(401)
			return
		}
		ty := typeRE.FindStringSubmatch(string(b))[1]
		a.bodies[ty] = append(a.bodies[ty], string(b))
		switch ty {
		case "10":
			w.WriteHeader(a.status)
			if a.status != 200 {
				_, _ = w.Write([]byte(`<result><errors><property id="9"><error code="2101" description="Default rate is required but does not exist"/></property></errors></result>`))
				return
			}
			_, _ = w.Write([]byte(`<result TUID="x" timestamp="1"/>`))
		case "9":
			_, _ = w.Write([]byte(a.listBody))
			a.listBody = `<result timestamp="1"><bookings/></result>` // later windows are empty
		case "4":
			_, _ = w.Write([]byte(`<result><bookings>
<booking booking_id="1001" status="ConfirmBooking" arrival="2026-12-10" departure="2026-12-12" room_id="R1" rateplan_id="P1" adults="2" children="1"><customer first_name="Kim" last_name="Park" email="kim@x.com"/></booking>
<booking booking_id="1002" status="CancelBooking" arrival="2026-12-20" departure="2026-12-21" room_id="R1" rateplan_id="P1" adults="2" children="0"><customer first_name="Li" last_name="Wu"/></booking>
</bookings></result>`))
		case "5":
			_, _ = w.Write([]byte(`<result><property id="9" name="Sea View" currency="USD"><rooms><room room_id="R1" room_name="Deluxe Double"/></rooms><rateplans><rateplan rateplan_id="P1" rateplan_name="Flexible rate"/></rateplans><products><product room_id="R1" rateplan_id="P1"/></products></property></result>`))
		}
	}))
	t.Cleanup(a.srv.Close)
	return a
}

func (a *agodaServer) adapter() *Agoda {
	return NewAgoda(a.srv.URL, "9", "cid:csecret", a.srv.Client())
}

func TestAgodaGetsOneTokenAndPushesRatesAndInventoryInOneRequest(t *testing.T) {
	a := newAgoda(t)
	ad := a.adapter()
	slots := []Slot{
		{ExternalRoomID: "R1", ExternalRateID: "P1", Date: "2026-12-10", Available: 2, Price: 12345, Currency: "USD"},
		{ExternalRoomID: "R1", ExternalRateID: "P1", Date: "2026-12-11", Available: 0, Price: 12345, Currency: "USD"},
	}
	if err := ad.PushAvailability(context.Background(), slots); err != nil {
		t.Fatal(err)
	}
	if err := ad.PushAvailability(context.Background(), slots[:1]); err != nil {
		t.Fatal(err)
	}
	if a.tokens != 1 {
		t.Fatalf("%d token requests: one token serves an hour", a.tokens)
	}
	body := a.bodies["10"][0]
	for _, want := range []string{`property_id="9"`, `room_id="R1"`, `rateplan_id="P1"`, `<value>2026-12-10</value>`, `<allotment>2</allotment>`, `<allotment>0</allotment>`, `currency="USD"`, `default="123.45"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the request lacks %s:\n%s", want, body)
		}
	}
}

func TestAgodaErrorsAreReadFromTheAnswer(t *testing.T) {
	a := newAgoda(t)
	a.status = 400
	err := a.adapter().PushAvailability(context.Background(), []Slot{{ExternalRoomID: "R1", ExternalRateID: "P1", Date: "2026-12-10", Available: 1, Price: 100, Currency: "USD"}})
	if err == nil || !strings.Contains(err.Error(), "Default rate is required") {
		t.Fatalf("%v", err)
	}
	bad := NewAgoda(a.srv.URL, "9", "wrong:creds", a.srv.Client())
	if err := bad.PushAvailability(context.Background(), []Slot{{Date: "2026-12-10"}}); err == nil {
		t.Fatal("bad credentials were taken as success")
	}
}

func TestAgodaBookingsAreListedByTheHourThenReadInDetail(t *testing.T) {
	a := newAgoda(t)
	a.listBody = `<result timestamp="1"><bookings><booking booking_id="1001" status="ConfirmBooking"/><booking booking_id="1002" status="CancelBooking"/></bookings></result>`
	got, err := a.adapter().PullBookings(context.Background(), time.Now().Add(-150*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(a.bodies["9"]); n < 3 { // 150 minutes is at least three one-hour windows
		t.Fatalf("%d list calls: Agoda allows 60 minutes at a time", n)
	}
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	if b := got[0]; b.ExternalID != "1001" || b.Status != "new" || b.GuestName != "Kim Park" || b.Email != "kim@x.com" || b.ExternalRoomID != "R1" || b.ExternalRateID != "P1" || b.Adults != 2 || b.Children != 1 || b.Arrival != "2026-12-10" || b.Departure != "2026-12-12" {
		t.Fatalf("%+v", b)
	}
	if got[1].Status != "cancelled" {
		t.Fatalf("%+v", got[1])
	}
}

func TestAgodaListsRoomsAndRatesForMapping(t *testing.T) {
	a := newAgoda(t)
	ad := a.adapter()
	if err := ad.Test(context.Background()); err != nil {
		t.Fatal(err)
	}
	rem, err := ad.Remote(context.Background())
	if err != nil || len(rem) != 1 || rem[0].RoomID != "R1" || rem[0].RoomName != "Deluxe Double" || rem[0].RateID != "P1" || rem[0].RateName != "Flexible rate" {
		t.Fatalf("%+v %v", rem, err)
	}
}
