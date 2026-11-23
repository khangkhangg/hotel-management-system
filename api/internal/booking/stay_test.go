package booking_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/db"
	"hotelpms/api/internal/testkit"
)

// roomID returns the id of the room with the number, for the hotel's first property.
func (h *hotel) roomID(number string) string {
	h.w.T.Helper()
	var rows []struct{ ID, Number string }
	must(h.w.T, json.Unmarshal([]byte(h.mgr.Do("GET", "/api/rooms", &h.property, "").Want(h.w.T, 200).Body), &rows))
	for _, r := range rows {
		if r.Number == number {
			return r.ID
		}
	}
	h.w.T.Fatalf("no room %s", number)
	return ""
}

func (h *hotel) setRoomStatus(number, status string) {
	h.w.T.Helper()
	must(h.w.T, db.WithOrg(ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update room set status = $2 where number = $1`, number, status)
		return err
	}))
}

func (h *hotel) roomStatus(number string) (status string) {
	h.w.T.Helper()
	must(h.w.T, db.WithOrg(ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select status from room where number = $1`, number).Scan(&status)
	}))
	return
}

func status(t *testing.T, h *hotel, id string) string {
	t.Helper()
	var d struct{ Status string }
	must(t, json.Unmarshal([]byte(h.desk.Do("GET", "/api/reservations/"+id, &h.property, "").Want(t, 200).Body), &d))
	return d.Status
}

func TestAssigningARoomFollowsTheRules(t *testing.T) {
	h := newHotel(t, 2)
	b := newID(t, h.book(h.desk, "2026-12-10", "2026-12-12", "").Want(t, 201))
	suite := newID(t, h.mgr.Do("POST", "/api/room-types", &h.property, `{"name":"Suite","baseGuests":2,"maxGuests":4}`).Want(t, 201)).ID
	h.mgr.Do("POST", "/api/rooms", &h.property, fmt.Sprintf(`{"roomTypeId":%q,"number":"900","floor":9}`, suite)).Want(t, 201)

	h.desk.Do("POST", "/api/reservations/"+b.ID+"/assign", &h.property, fmt.Sprintf(`{"roomId":%q}`, h.roomID("900"))).Want(t, 409, "wrong_type")
	h.setRoomStatus("101", "dirty")
	h.desk.Do("POST", "/api/reservations/"+b.ID+"/assign", &h.property, fmt.Sprintf(`{"roomId":%q}`, h.roomID("101"))).Want(t, 409, "room_not_ready")
	h.desk.Do("POST", "/api/reservations/"+b.ID+"/assign", &h.property, fmt.Sprintf(`{"roomId":%q,"force":true}`, h.roomID("101"))).Want(t, 400, "reason")
	h.desk.Do("POST", "/api/reservations/"+b.ID+"/assign", &h.property, fmt.Sprintf(`{"roomId":%q,"force":true,"reason":"cleaner is on the way"}`, h.roomID("101"))).Want(t, 204)
	h.setRoomStatus("102", "out_of_order")
	other := newID(t, h.book(h.desk, "2026-12-20", "2026-12-21", "").Want(t, 201))
	h.desk.Do("POST", "/api/reservations/"+other.ID+"/assign", &h.property, fmt.Sprintf(`{"roomId":%q}`, h.roomID("102"))).Want(t, 409, "room_not_ready")
	h.desk.Do("POST", "/api/reservations/"+other.ID+"/assign", &h.property, `{"roomId":"not-an-id"}`).Want(t, 400)
	h.desk.Do("POST", "/api/reservations/"+other.ID+"/assign", &h.property, fmt.Sprintf(`{"roomId":%q}`, uuidString())).Want(t, 404)
}

func uuidString() string { return "0191a2b3-0000-7000-8000-000000000000" }

func TestTwoStaysCannotShareARoomButBackToBackStaysCan(t *testing.T) {
	h := newHotel(t, 3)
	room := h.roomID("101")
	assign := func(id string) testkit.Reply {
		return h.desk.Do("POST", "/api/reservations/"+id+"/assign", &h.property, fmt.Sprintf(`{"roomId":%q}`, room))
	}
	a := newID(t, h.book(h.desk, "2026-12-10", "2026-12-12", "").Want(t, 201))
	b := newID(t, h.book(h.desk, "2026-12-11", "2026-12-13", "").Want(t, 201))
	c := newID(t, h.book(h.desk, "2026-12-12", "2026-12-14", "").Want(t, 201)) // arrives the day a leaves
	d := newID(t, h.book(h.desk, "2026-12-08", "2026-12-10", "").Want(t, 201)) // leaves the day a arrives
	assign(a.ID).Want(t, 204)
	assign(b.ID).Want(t, 409, "room_taken")
	assign(c.ID).Want(t, 204)
	assign(d.ID).Want(t, 204)
	// A cancelled booking gives its room back.
	h.desk.Do("POST", "/api/reservations/"+a.ID+"/cancel", &h.property, "").Want(t, 204)
	assign(b.ID).Want(t, 409, "room_taken") // c still holds the 12th
}

func TestCheckInAndCheckOut(t *testing.T) {
	h := newHotel(t, 2)
	b := newID(t, h.book(h.desk, "2026-11-23", "2026-11-25", "").Want(t, 201))
	path := func(p string) string { return "/api/reservations/" + b.ID + "/" + p }
	h.desk.Do("POST", path("check-in"), &h.property, "").Want(t, 409, "no_room")
	h.desk.Do("POST", path("check-out"), &h.property, "").Want(t, 409, "not_checked_in")
	h.desk.Do("POST", path("assign"), &h.property, fmt.Sprintf(`{"roomId":%q}`, h.roomID("101"))).Want(t, 204)
	h.desk.Do("POST", path("check-in"), &h.property, "").Want(t, 204)
	if s := status(t, h, b.ID); s != "checked_in" {
		t.Fatalf("status %s", s)
	}
	h.desk.Do("POST", path("check-in"), &h.property, "").Want(t, 409, "already_checked_in")
	h.desk.Do("POST", "/api/reservations/"+b.ID+"/cancel", &h.property, "").Want(t, 409, "cannot_cancel")
	h.desk.Do("POST", path("check-out"), &h.property, "").Want(t, 204)
	if s := status(t, h, b.ID); s != "checked_out" {
		t.Fatalf("status %s", s)
	}
	if got := h.roomStatus("101"); got != "dirty" {
		t.Fatalf("room is %s after checkout", got)
	}
	h.desk.Do("POST", path("check-out"), &h.property, "").Want(t, 409, "not_checked_in")
}

func TestCheckingInBeforeArrivalDayIsRefused(t *testing.T) {
	h := newHotel(t, 1)
	b := newID(t, h.book(h.desk, "2026-12-10", "2026-12-11", "").Want(t, 201))
	h.desk.Do("POST", "/api/reservations/"+b.ID+"/assign", &h.property, fmt.Sprintf(`{"roomId":%q}`, h.roomID("101"))).Want(t, 204)
	h.desk.Do("POST", "/api/reservations/"+b.ID+"/check-in", &h.property, "").Want(t, 409, "too_early")
	h.w.Advance(17 * 24 * time.Hour) // the 10th
	h.desk.Do("POST", "/api/reservations/"+b.ID+"/check-in", &h.property, "").Want(t, 204)
}

func TestLeavingEarlyFreesTheRoomForTheRestOfTheStay(t *testing.T) {
	h := newHotel(t, 1)
	b := newID(t, h.book(h.desk, "2026-11-23", "2026-11-27", "").Want(t, 201))
	h.desk.Do("POST", "/api/reservations/"+b.ID+"/assign", &h.property, fmt.Sprintf(`{"roomId":%q}`, h.roomID("101"))).Want(t, 204)
	h.desk.Do("POST", "/api/reservations/"+b.ID+"/check-in", &h.property, "").Want(t, 204)
	h.w.Advance(48 * time.Hour) // the 25th
	h.desk.Do("POST", "/api/reservations/"+b.ID+"/check-out", &h.property, "").Want(t, 204)
	for date, want := range map[string]int{"2026-11-23": 1, "2026-11-24": 1, "2026-11-25": 0, "2026-11-26": 0} {
		if s, _ := h.sold(date); s != want {
			t.Fatalf("%s: sold %d, want %d", date, s, want)
		}
	}
	// The room can be sold again from the day the guest left, and the same room can take it.
	n := newID(t, h.book(h.desk, "2026-11-25", "2026-11-27", "").Want(t, 201))
	h.desk.Do("POST", "/api/reservations/"+n.ID+"/assign", &h.property, fmt.Sprintf(`{"roomId":%q,"force":true,"reason":"cleaned"}`, h.roomID("101"))).Want(t, 204)
}

func TestNoShowReleasesTheRoomOnceTheDayHasCome(t *testing.T) {
	h := newHotel(t, 1)
	b := newID(t, h.book(h.desk, "2026-11-24", "2026-11-26", "").Want(t, 201))
	h.desk.Do("POST", "/api/reservations/"+b.ID+"/no-show", &h.property, "").Want(t, 409, "too_early")
	h.w.Advance(24 * time.Hour)
	h.desk.Do("POST", "/api/reservations/"+b.ID+"/no-show", &h.property, "").Want(t, 204)
	if s, _ := h.sold("2026-11-24"); s != 0 {
		t.Fatalf("sold %d after a no-show", s)
	}
	h.desk.Do("POST", "/api/reservations/"+b.ID+"/no-show", &h.property, "").Want(t, 409, "not_confirmed")
	h.desk.Do("POST", "/api/reservations/"+b.ID+"/cancel", &h.property, "").Want(t, 409, "cannot_cancel")
}

func TestMovingToAnotherRoomTypeMovesTheInventory(t *testing.T) {
	h := newHotel(t, 1)
	suite := newID(t, h.mgr.Do("POST", "/api/room-types", &h.property, `{"name":"Suite","baseGuests":2,"maxGuests":4}`).Want(t, 201)).ID
	h.mgr.Do("POST", "/api/rooms", &h.property, fmt.Sprintf(`{"roomTypeId":%q,"number":"900","floor":9}`, suite)).Want(t, 201)
	plan := newID(t, h.mgr.Do("POST", "/api/rate-plans", &h.property, fmt.Sprintf(`{"roomTypeId":%q,"name":"Flexible"}`, suite)).Want(t, 201)).ID
	h.mgr.Do("PUT", "/api/rates", &h.property, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-11-23","to":"2027-01-01","price":25000}`, plan)).Want(t, 200)
	b := newID(t, h.book(h.desk, "2026-12-10", "2026-12-12", "").Want(t, 201))
	h.desk.Do("POST", "/api/reservations/"+b.ID+"/assign", &h.property, fmt.Sprintf(`{"roomId":%q}`, h.roomID("101"))).Want(t, 204)

	h.desk.Do("POST", "/api/reservations/"+b.ID+"/move", &h.property, fmt.Sprintf(`{"roomTypeId":%q,"ratePlanId":%q}`, suite, plan)).Want(t, 204)
	if s, _ := h.sold("2026-12-10"); s != 0 {
		t.Fatalf("the old room type is still sold: %d", s)
	}
	var sold int
	must(t, db.WithOrg(ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select sold from inventory where room_type_id = $1 and date = '2026-12-10'`, suite).Scan(&sold)
	}))
	if sold != 1 {
		t.Fatalf("the new room type is not sold: %d", sold)
	}
	got := h.desk.Do("GET", "/api/reservations/"+b.ID, &h.property, "").Want(t, 200, `"total":50000`, `"roomNumber":null`)
	_ = got
	// Another booking takes the only suite; now a move into it is refused and nothing changes.
	other := newID(t, h.book(h.desk, "2026-12-10", "2026-12-12", "").Want(t, 201))
	h.desk.Do("POST", "/api/reservations/"+other.ID+"/move", &h.property, fmt.Sprintf(`{"roomTypeId":%q,"ratePlanId":%q}`, suite, plan)).Want(t, 409, "sold_out")
	if s, _ := h.sold("2026-12-10"); s != 1 {
		t.Fatalf("a refused move changed inventory: %d", s)
	}
}

func TestStayActionsNeedThePermissions(t *testing.T) {
	h := newHotel(t, 1)
	b := newID(t, h.book(h.desk, "2026-11-23", "2026-11-24", "").Want(t, 201))
	hk := h.w.Member(h.w.OrgA, &h.property, "housekeeping")
	rev := h.w.Member(h.w.OrgA, &h.property, "revenue")
	for _, p := range []string{"assign", "check-in", "check-out", "no-show", "move"} {
		hk.Do("POST", "/api/reservations/"+b.ID+"/"+p, &h.property, "{}").Want(t, 403)
		rev.Do("POST", "/api/reservations/"+b.ID+"/"+p, &h.property, "{}").Want(t, 403)
	}
	h.w.Anon("POST", "/api/reservations/"+b.ID+"/check-in", "").Want(t, 401)
}
