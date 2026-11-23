package housekeeping_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/housekeeping"
	"hotelpms/api/internal/rooms"
	"hotelpms/api/internal/testkit"
)

var ctx = testkit.Ctx

func must(t *testing.T, err error) { t.Helper(); testkit.Must(t, err) }

type hotel struct {
	w                 *testkit.World
	mgr, desk, keeper *testkit.Person
	prop              uuid.UUID
	rt, rp            string
}

func idOf(t *testing.T, r testkit.Reply) string {
	t.Helper()
	var b struct{ ID string }
	must(t, json.Unmarshal([]byte(r.Body), &b))
	return b.ID
}

func newHotel(t *testing.T, nRooms int) *hotel {
	w := testkit.New(t, rooms.Mount, booking.Mount, housekeeping.Mount)
	h := &hotel{w: w, prop: w.A1}
	h.mgr = w.Member(w.OrgA, &w.A1, "manager")
	h.desk = w.Member(w.OrgA, &w.A1, "front_desk")
	h.keeper = w.Member(w.OrgA, &w.A1, "housekeeping")
	h.rt = idOf(t, h.mgr.Do("POST", "/api/room-types", &h.prop, `{"name":"Deluxe","baseGuests":2,"maxGuests":3}`).Want(t, 201))
	for i := 0; i < nRooms; i++ {
		h.mgr.Do("POST", "/api/rooms", &h.prop, fmt.Sprintf(`{"roomTypeId":%q,"number":"%d","floor":%d}`, h.rt, 101+i, 1+i/10)).Want(t, 201)
	}
	h.rp = idOf(t, h.mgr.Do("POST", "/api/rate-plans", &h.prop, fmt.Sprintf(`{"roomTypeId":%q,"name":"Flexible"}`, h.rt)).Want(t, 201))
	h.mgr.Do("PUT", "/api/rates", &h.prop, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-11-23","to":"2027-01-01","price":10000}`, h.rp)).Want(t, 200)
	return h
}

func (h *hotel) roomID(number string) string {
	h.w.T.Helper()
	var rows []struct{ ID, Number string }
	must(h.w.T, json.Unmarshal([]byte(h.mgr.Do("GET", "/api/rooms", &h.prop, "").Want(h.w.T, 200).Body), &rows))
	for _, r := range rows {
		if r.Number == number {
			return r.ID
		}
	}
	h.w.T.Fatalf("no room %s", number)
	return ""
}

func (h *hotel) book(from, to string) string {
	h.w.T.Helper()
	return idOf(h.w.T, h.desk.Do("POST", "/api/reservations", &h.prop, fmt.Sprintf(
		`{"guest":{"name":"Mai Pham","email":"mai@hotel.com","phone":"+84 90 111 2222"},"rooms":[{"roomTypeId":%q,"ratePlanId":%q,"from":%q,"to":%q,"adults":2,"children":0}]}`, h.rt, h.rp, from, to)).Want(h.w.T, 201))
}

func (h *hotel) stay(number string) string {
	h.w.T.Helper()
	res := h.book("2026-11-23", "2026-11-24")
	h.desk.Do("POST", "/api/reservations/"+res+"/assign", &h.prop, fmt.Sprintf(`{"roomId":%q}`, h.roomID(number))).Want(h.w.T, 204)
	h.desk.Do("POST", "/api/reservations/"+res+"/check-in", &h.prop, "").Want(h.w.T, 204)
	return res
}

type task struct {
	ID         string
	RoomNumber string
	Status     string
	RoomStatus string
}

func (h *hotel) tasks(p *testkit.Person) []task {
	h.w.T.Helper()
	var out []task
	must(h.w.T, json.Unmarshal([]byte(p.Do("GET", "/api/housekeeping/tasks?date=2026-11-23", &h.prop, "").Want(h.w.T, 200).Body), &out))
	return out
}

func TestCheckingOutCreatesOneCleaningTaskPerRoom(t *testing.T) {
	h := newHotel(t, 2)
	res := h.stay("101")
	h.desk.Do("POST", "/api/reservations/"+res+"/check-out", &h.prop, "").Want(t, 204)
	got := h.tasks(h.keeper)
	if len(got) != 1 || got[0].RoomNumber != "101" || got[0].Status != "todo" || got[0].RoomStatus != "dirty" {
		t.Fatalf("%+v", got)
	}
	// A second guest leaving the same room the same day does not add another open task.
	res2 := h.book("2026-11-23", "2026-11-24")
	h.desk.Do("POST", "/api/reservations/"+res2+"/assign", &h.prop, fmt.Sprintf(`{"roomId":%q,"force":true,"reason":"test"}`, h.roomID("101"))).Want(t, 204)
	h.desk.Do("POST", "/api/reservations/"+res2+"/check-in", &h.prop, "").Want(t, 204)
	h.desk.Do("POST", "/api/reservations/"+res2+"/check-out", &h.prop, "").Want(t, 204)
	if got := h.tasks(h.keeper); len(got) != 1 {
		t.Fatalf("%d tasks", len(got))
	}
}

func TestHousekeepingNeverSeesGuestDataOrMoney(t *testing.T) {
	h := newHotel(t, 2)
	res := h.stay("101")
	h.desk.Do("POST", "/api/reservations/"+res+"/check-out", &h.prop, "").Want(t, 204)
	other := h.book("2026-11-23", "2026-11-25") // an arrival today in the room just left
	h.desk.Do("POST", "/api/reservations/"+other+"/assign", &h.prop, fmt.Sprintf(`{"roomId":%q,"force":true,"reason":"cleaning now"}`, h.roomID("101"))).Want(t, 204)
	h.keeper.Do("POST", "/api/maintenance-tickets", &h.prop, fmt.Sprintf(`{"roomId":%q,"description":"Tap drips"}`, h.roomID("101"))).Want(t, 201)
	for _, path := range []string{"/api/housekeeping/tasks?date=2026-11-23", "/api/maintenance-tickets", "/api/rooms"} {
		body := h.keeper.Do("GET", path, &h.prop, "").Want(t, 200).Body
		for _, secret := range []string{"Mai", "mai@", "+84", "10000", "20000", "price", "total", "guest"} {
			if strings.Contains(body, secret) {
				t.Fatalf("%s shows %q to housekeeping: %s", path, secret, body)
			}
		}
	}
	if !strings.Contains(h.keeper.Do("GET", "/api/housekeeping/tasks?date=2026-11-23", &h.prop, "").Body, `"arrivalToday":true`) {
		t.Fatal("housekeeping is not told which room has an arrival today")
	}
}

func TestAHousekeeperWorksATaskThroughToClean(t *testing.T) {
	h := newHotel(t, 1)
	res := h.stay("101")
	h.desk.Do("POST", "/api/reservations/"+res+"/check-out", &h.prop, "").Want(t, 204)
	id := h.tasks(h.keeper)[0].ID
	h.keeper.Do("PATCH", "/api/housekeeping/tasks/"+id, &h.prop, `{"status":"in_progress"}`).Want(t, 204)
	if got := h.tasks(h.keeper)[0]; got.Status != "in_progress" || got.RoomStatus != "dirty" {
		t.Fatalf("%+v", got)
	}
	h.keeper.Do("PATCH", "/api/housekeeping/tasks/"+id, &h.prop, `{"status":"done"}`).Want(t, 204)
	if got := h.tasks(h.keeper)[0]; got.Status != "done" || got.RoomStatus != "clean" {
		t.Fatalf("%+v", got)
	}
	h.keeper.Do("PATCH", "/api/housekeeping/tasks/"+id, &h.prop, `{"status":"flying"}`).Want(t, 400)
	h.keeper.Do("PATCH", "/api/housekeeping/tasks/"+uuid.NewString(), &h.prop, `{"status":"done"}`).Want(t, 404)
	h.desk.Do("PATCH", "/api/housekeeping/tasks/"+id, &h.prop, `{"status":"todo"}`).Want(t, 403)
	rev := h.w.Member(h.w.OrgA, &h.prop, "revenue")
	rev.Do("GET", "/api/housekeeping/tasks?date=2026-11-23", &h.prop, "").Want(t, 403)
}

func TestRoomStatusCanBeSetButNotToOutOfOrder(t *testing.T) {
	h := newHotel(t, 1)
	room := h.roomID("101")
	for _, s := range []string{"dirty", "inspect", "clean"} {
		h.keeper.Do("PATCH", "/api/rooms/"+room+"/status", &h.prop, fmt.Sprintf(`{"status":%q}`, s)).Want(t, 204)
	}
	h.keeper.Do("PATCH", "/api/rooms/"+room+"/status", &h.prop, `{"status":"out_of_order"}`).Want(t, 400)
	h.desk.Do("PATCH", "/api/rooms/"+room+"/status", &h.prop, `{"status":"dirty"}`).Want(t, 403)
}

func (h *hotel) blocked(date string) (blocked, total int) {
	must(h.w.T, db.WithOrg(ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select blocked, total from inventory where room_type_id = $1 and date = $2`, h.rt, date).Scan(&blocked, &total)
	}))
	return
}

func TestABlockingTicketTakesTheRoomOutOfSaleUntilItIsClosed(t *testing.T) {
	h := newHotel(t, 3)
	r := h.keeper.Do("POST", "/api/maintenance-tickets", &h.prop, fmt.Sprintf(`{"roomId":%q,"description":"Ceiling leak","blocksFrom":"2026-12-10","blocksTo":"2026-12-13"}`, h.roomID("101"))).Want(t, 201)
	ticket := idOf(t, r)
	for date, want := range map[string]int{"2026-12-09": 0, "2026-12-10": 1, "2026-12-12": 1, "2026-12-13": 0} {
		if b, _ := h.blocked(date); b != want {
			t.Fatalf("%s: blocked %d, want %d", date, b, want)
		}
	}
	// Only two rooms can be sold on a blocked night.
	h.book("2026-12-11", "2026-12-12")
	h.book("2026-12-11", "2026-12-12")
	h.desk.Do("POST", "/api/reservations", &h.prop, fmt.Sprintf(`{"guest":{"name":"X"},"rooms":[{"roomTypeId":%q,"ratePlanId":%q,"from":"2026-12-11","to":"2026-12-12","adults":1,"children":0}]}`, h.rt, h.rp)).Want(t, 409, "sold_out")
	h.mgr.Do("PATCH", "/api/maintenance-tickets/"+ticket, &h.prop, `{"status":"done","resolution":"Roof fixed"}`).Want(t, 204)
	if b, _ := h.blocked("2026-12-11"); b != 0 {
		t.Fatalf("still blocked after closing: %d", b)
	}
	h.mgr.Do("PATCH", "/api/maintenance-tickets/"+ticket, &h.prop, `{"status":"done"}`).Want(t, 409, "already_closed")
	if b, _ := h.blocked("2026-12-11"); b != 0 {
		t.Fatalf("closing twice changed the block: %d", b)
	}
}

func TestATicketThatClashesWithABookingNamesIt(t *testing.T) {
	h := newHotel(t, 2)
	res := h.book("2026-12-10", "2026-12-12")
	h.desk.Do("POST", "/api/reservations/"+res+"/assign", &h.prop, fmt.Sprintf(`{"roomId":%q}`, h.roomID("101"))).Want(t, 204)
	got := h.mgr.Do("POST", "/api/maintenance-tickets", &h.prop, fmt.Sprintf(`{"roomId":%q,"description":"Floor","blocksFrom":"2026-12-11","blocksTo":"2026-12-14"}`, h.roomID("101"))).Want(t, 201, `"clashes":["`+res)
	_ = got
	// Without dates it blocks nothing and clashes with nothing.
	h.mgr.Do("POST", "/api/maintenance-tickets", &h.prop, fmt.Sprintf(`{"roomId":%q,"description":"Squeaky door"}`, h.roomID("102"))).Want(t, 201, `"clashes":[]`)
}

func TestTicketInputIsChecked(t *testing.T) {
	h := newHotel(t, 1)
	room := h.roomID("101")
	for _, bad := range []string{
		fmt.Sprintf(`{"roomId":%q,"description":""}`, room),
		fmt.Sprintf(`{"roomId":%q,"description":"x","blocksFrom":"2026-12-12","blocksTo":"2026-12-10"}`, room),
		fmt.Sprintf(`{"roomId":%q,"description":"x","blocksFrom":"2026-12-12"}`, room),
		fmt.Sprintf(`{"roomId":%q,"description":"x","blocksFrom":"2026-12-01","blocksTo":"2028-12-01"}`, room),
		`{"roomId":"nope","description":"x"}`,
	} {
		h.keeper.Do("POST", "/api/maintenance-tickets", &h.prop, bad).Want(t, 400)
	}
	h.keeper.Do("POST", "/api/maintenance-tickets", &h.prop, fmt.Sprintf(`{"roomId":%q,"description":"x"}`, uuid.NewString())).Want(t, 404)
	h.desk.Do("POST", "/api/maintenance-tickets", &h.prop, fmt.Sprintf(`{"roomId":%q,"description":"x"}`, room)).Want(t, 403)
	h.w.Anon("GET", "/api/maintenance-tickets", "").Want(t, 401)
	other := h.w.Member(h.w.OrgB, &h.w.B1, "housekeeping")
	other.Do("PATCH", "/api/rooms/"+room+"/status", &h.w.B1, `{"status":"dirty"}`).Want(t, 404)
	_ = time.Now
}

func TestARoomUnderABlockingTicketIsNeverAssigned(t *testing.T) {
	h := newHotel(t, 2)
	h.keeper.Do("POST", "/api/maintenance-tickets", &h.prop, fmt.Sprintf(`{"roomId":%q,"description":"Ceiling leak","blocksFrom":"2026-12-10","blocksTo":"2026-12-13"}`, h.roomID("101"))).Want(t, 201)
	res := h.book("2026-12-11", "2026-12-12")
	h.desk.Do("POST", "/api/reservations/"+res+"/assign", &h.prop, fmt.Sprintf(`{"roomId":%q}`, h.roomID("101"))).Want(t, 409, "room_not_ready")
	h.desk.Do("POST", "/api/reservations/"+res+"/assign", &h.prop, fmt.Sprintf(`{"roomId":%q,"force":true,"reason":"try"}`, h.roomID("101"))).Want(t, 409, "room_not_ready")
	h.desk.Do("POST", "/api/reservations/"+res+"/assign", &h.prop, fmt.Sprintf(`{"roomId":%q}`, h.roomID("102"))).Want(t, 204)
	// Nights outside the block are fine.
	later := h.book("2026-12-14", "2026-12-15")
	h.desk.Do("POST", "/api/reservations/"+later+"/assign", &h.prop, fmt.Sprintf(`{"roomId":%q}`, h.roomID("101"))).Want(t, 204)
}
