package guestbooking_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAGuestAndTheHotelTalkAboutOneBookingAndEachSeesWhatTheOtherSent(t *testing.T) {
	h := newHotel(t, 2)
	desk := h.w.Member(h.w.OrgA, h.prop, "front_desk")
	keeper := h.w.Member(h.w.OrgA, h.prop, "housekeeping")
	m := h.book(t, "hotel", "2026-12-10", "2026-12-12")
	tok := h.link(t, m.Code, "mai@hotel.com")
	h.w.Anon("GET", "/api/manage/"+tok+"/messages", "").Want(t, 200, "[]")
	for _, bad := range []string{`{"body":""}`, `{"body":"   "}`, `{"body":"` + strings.Repeat("a", 2001) + `"}`, `{}`} {
		h.w.Anon("POST", "/api/manage/"+tok+"/messages", bad).Want(t, 400)
	}
	h.w.Anon("POST", "/api/manage/"+tok+"/messages", `{"body":"Can we check in early?"}`).Want(t, 201)

	keeper.Do("GET", "/api/messages", h.prop, "").Want(t, 403)
	list := desk.Do("GET", "/api/messages", h.prop, "").Want(t, 200, m.Code, "Can we check in early?", `"unread":1`).Body
	var threads []struct{ ReservationID string }
	if err := json.Unmarshal([]byte(list), &threads); err != nil || len(threads) != 1 {
		t.Fatalf("%v %s", err, list)
	}
	res := threads[0].ReservationID
	desk.Do("GET", "/api/reservations/"+res+"/messages", h.prop, "").Want(t, 200, `"sender":"guest"`, "Can we check in early?")
	desk.Do("GET", "/api/messages", h.prop, "").Want(t, 200, `"unread":0`)

	before := len(h.w.Mail.Sent())
	keeper.Do("POST", "/api/reservations/"+res+"/messages", h.prop, `{"body":"x"}`).Want(t, 403)
	desk.Do("POST", "/api/reservations/"+res+"/messages", h.prop, `{"body":"Yes, from 12:00"}`).Want(t, 201)
	sent := h.w.Mail.Sent()
	if len(sent) != before+1 || sent[len(sent)-1].To != "mai@hotel.com" || !strings.Contains(sent[len(sent)-1].Text, "Yes, from 12:00") {
		t.Fatalf("the guest was not told: %+v", sent[before:])
	}
	h.w.Anon("GET", "/api/manage/"+tok+"/messages", "").Want(t, 200, `"sender":"staff"`, "Yes, from 12:00", "Can we check in early?")
	desk.Do("POST", "/api/reservations/"+res+"/messages", h.prop, `{"body":""}`).Want(t, 400)
}

func TestMessagesBelongToOneBookingAndOneProperty(t *testing.T) {
	h := newHotel(t, 2)
	desk := h.w.Member(h.w.OrgA, h.prop, "front_desk")
	m := h.book(t, "hotel", "2026-12-10", "2026-12-12")
	tok := h.link(t, m.Code, "mai@hotel.com")
	h.w.Anon("POST", "/api/manage/"+tok+"/messages", `{"body":"hello"}`).Want(t, 201)
	res := h.row(t, `select id::text from reservation where code = $1`, m.Code)
	other := h.w.A2
	owner := h.w.Member(h.w.OrgA, nil, "owner")
	owner.Do("GET", "/api/reservations/"+res+"/messages", &other, "").Want(t, 404)
	owner.Do("POST", "/api/reservations/"+res+"/messages", &other, `{"body":"x"}`).Want(t, 404)
	desk.Do("GET", "/api/reservations/"+uuid.NewString()+"/messages", h.prop, "").Want(t, 404)
	h.w.Anon("GET", "/api/manage/nope.nope/messages", "").Want(t, 404)
}
