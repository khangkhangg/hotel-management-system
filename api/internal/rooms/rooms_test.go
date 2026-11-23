package rooms_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/db"
	"hotelpms/api/internal/rooms"
	"hotelpms/api/internal/testkit"
)

var ctx = testkit.Ctx

func must(t *testing.T, err error) { t.Helper(); testkit.Must(t, err) }

type created struct{ ID string }

func id(t *testing.T, r testkit.Reply) string {
	t.Helper()
	var c created
	must(t, json.Unmarshal([]byte(r.Body), &c))
	return c.ID
}

func setup(t *testing.T) (*testkit.World, *testkit.Person, *testkit.Person) {
	w := testkit.New(t, rooms.Mount)
	mgr := w.Member(w.OrgA, &w.A1, "manager")
	desk := w.Member(w.OrgA, &w.A1, "front_desk")
	return w, mgr, desk
}

func roomType(t *testing.T, w *testkit.World, mgr *testkit.Person, name string) string {
	t.Helper()
	return id(t, mgr.Do("POST", "/api/room-types", &w.A1, fmt.Sprintf(`{"name":%q,"baseGuests":2,"maxGuests":4,"extraAdultPrice":2000,"extraChildPrice":1000}`, name)).Want(t, 201))
}

func TestRoomsRaiseInventoryForTheNextYear(t *testing.T) {
	w, mgr, _ := setup(t)
	rt := roomType(t, w, mgr, "Deluxe")
	for _, n := range []string{"101", "102", "103"} {
		mgr.Do("POST", "/api/rooms", &w.A1, fmt.Sprintf(`{"roomTypeId":%q,"number":%q,"floor":1}`, rt, n)).Want(t, 201)
	}
	var days, total, minTotal int
	must(t, db.WithOrg(ctx, w.Pool, w.OrgA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select count(*), max(total), min(total) from inventory where room_type_id = $1`, rt).Scan(&days, &total, &minTotal)
	}))
	if days < 365 || total != 3 || minTotal != 3 {
		t.Fatalf("%d days, total %d..%d", days, minTotal, total)
	}
	mgr.Do("POST", "/api/rooms", &w.A1, fmt.Sprintf(`{"roomTypeId":%q,"number":"101","floor":1}`, rt)).Want(t, 409, "room_number_taken")
}

func TestOnlyManagersConfigureRoomsAndOthersCanRead(t *testing.T) {
	w, mgr, desk := setup(t)
	roomType(t, w, mgr, "Deluxe")
	desk.Do("POST", "/api/room-types", &w.A1, `{"name":"x","baseGuests":1,"maxGuests":1}`).Want(t, 403)
	desk.Do("GET", "/api/room-types", &w.A1, "").Want(t, 200, "Deluxe")
	hk := w.Member(w.OrgA, &w.A1, "housekeeping")
	hk.Do("GET", "/api/rooms", &w.A1, "").Want(t, 200)
	w.Anon("GET", "/api/rooms", "").Want(t, 401)
}

func TestAnotherOrganizationsRoomsAreInvisible(t *testing.T) {
	w, mgr, _ := setup(t)
	suite := roomType(t, w, mgr, "Suite")
	other := w.Member(w.OrgB, &w.B1, "manager")
	got := other.Do("GET", "/api/room-types", &w.B1, "").Want(t, 200)
	if got.Body != "[]\n" {
		t.Fatalf("organization B sees %s", got.Body)
	}
	other.Do("PATCH", "/api/room-types/"+suite, &w.B1, `{"name":"stolen"}`).Want(t, 404)
}

func TestRoomTypeInputIsChecked(t *testing.T) {
	w, mgr, _ := setup(t)
	for _, bad := range []string{`{"name":"","baseGuests":2,"maxGuests":2}`, `{"name":"x","baseGuests":3,"maxGuests":2}`, `{"name":"x","baseGuests":0,"maxGuests":2}`, `{"name":"x","baseGuests":1,"maxGuests":2,"extraAdultPrice":-1}`, `{bad`} {
		mgr.Do("POST", "/api/room-types", &w.A1, bad).Want(t, 400)
	}
	mgr.Do("POST", "/api/room-types", nil, `{"name":"x","baseGuests":1,"maxGuests":2}`).Want(t, 403) // a manager must name a property
	w.Member(w.OrgA, nil, "owner").Do("POST", "/api/room-types", nil, `{"name":"x","baseGuests":1,"maxGuests":2}`).Want(t, 400)
}

func plan(t *testing.T, w *testkit.World, mgr *testkit.Person, rt string) string {
	t.Helper()
	return id(t, mgr.Do("POST", "/api/rate-plans", &w.A1, fmt.Sprintf(`{"roomTypeId":%q,"name":"Flexible","freeCancelUntilDays":2,"freeCancelUntilTime":"18:00","lateCancelPenalty":"first_night"}`, rt)).Want(t, 201))
}

type rateRow struct {
	Date            string
	Price           int64
	MinNights       int
	ClosedToArrival bool
}

func TestRatesAreSetPerDateAndRead(t *testing.T) {
	w, mgr, _ := setup(t)
	rp := plan(t, w, mgr, roomType(t, w, mgr, "Deluxe"))
	mgr.Do("PUT", "/api/rates", &w.A1, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-12-01","to":"2026-12-05","price":10000,"minNights":2,"closedToArrival":true}`, rp)).Want(t, 200)
	read := func() []rateRow {
		var rows []rateRow
		got := mgr.Do("GET", "/api/rates?ratePlanId="+rp+"&from=2026-12-01&to=2026-12-05", &w.A1, "").Want(t, 200)
		must(t, json.Unmarshal([]byte(got.Body), &rows))
		return rows
	}
	rows := read()
	if len(rows) != 4 || rows[0].Date != "2026-12-01" || rows[3].Date != "2026-12-04" || rows[0].Price != 10000 || rows[0].MinNights != 2 || !rows[0].ClosedToArrival {
		t.Fatalf("%+v", rows)
	}
	// Setting a range again changes those dates and nothing else.
	mgr.Do("PUT", "/api/rates", &w.A1, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-12-02","to":"2026-12-03","price":12000,"minNights":1}`, rp)).Want(t, 200)
	rows = read()
	if len(rows) != 4 || rows[0].Price != 10000 || rows[1].Price != 12000 || rows[2].Price != 10000 {
		t.Fatalf("%+v", rows)
	}
}

func TestRateInputIsChecked(t *testing.T) {
	w, mgr, _ := setup(t)
	rp := plan(t, w, mgr, roomType(t, w, mgr, "Deluxe"))
	for _, bad := range []string{
		fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-12-05","to":"2026-12-01","price":1}`, rp),
		fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-12-01","to":"2026-12-05","price":-1}`, rp),
		fmt.Sprintf(`{"ratePlanId":%q,"from":"yesterday","to":"2026-12-05","price":1}`, rp),
		fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-12-01","to":"2028-12-05","price":1}`, rp),
		`{"ratePlanId":"` + uuid.New().String() + `","from":"2026-12-01","to":"2026-12-05","price":1}`,
	} {
		if r := mgr.Do("PUT", "/api/rates", &w.A1, bad); r.Status != 400 && r.Status != 404 {
			t.Fatalf("%s: %d", bad, r.Status)
		}
	}
	desk := w.Member(w.OrgA, &w.A1, "front_desk")
	desk.Do("PUT", "/api/rates", &w.A1, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-12-01","to":"2026-12-02","price":1}`, rp)).Want(t, 403)
}

func TestAWildlyOffRateIsSavedButFlagged(t *testing.T) {
	w, mgr, _ := setup(t)
	rp := plan(t, w, mgr, roomType(t, w, mgr, "Deluxe"))
	set := func(from, to string, price int) testkit.Reply {
		return mgr.Do("PUT", "/api/rates", &w.A1, fmt.Sprintf(`{"ratePlanId":%q,"from":%q,"to":%q,"price":%d}`, rp, from, to, price)).Want(t, 200)
	}
	set("2026-12-01", "2026-12-11", 10000).Want(t, 200, `"suspicious":[]`)
	set("2026-12-20", "2026-12-21", 1000).Want(t, 200, "2026-12-20")
	set("2026-12-22", "2026-12-23", 40000).Want(t, 200, "2026-12-22")
	set("2026-12-24", "2026-12-25", 12000).Want(t, 200, `"suspicious":[]`)
}

func TestPriceAddsExtraGuestsAndRefusesTooMany(t *testing.T) {
	w, mgr, _ := setup(t)
	rp := plan(t, w, mgr, roomType(t, w, mgr, "Deluxe")) // base 2, max 4, extra adult 2000, extra child 1000
	mgr.Do("PUT", "/api/rates", &w.A1, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-12-01","to":"2026-12-04","price":10000}`, rp)).Want(t, 200)
	price := func(adults, children int) (int64, error) {
		var total int64
		err := db.WithOrg(ctx, w.Pool, w.OrgA, func(tx pgx.Tx) error {
			var e error
			_, total, e = rooms.Price(ctx, tx, uuid.MustParse(rp), date("2026-12-01"), date("2026-12-04"), adults, children)
			return e
		})
		return total, err
	}
	for _, c := range []struct {
		adults, children int
		want             int64
	}{{2, 0, 30000}, {1, 0, 30000}, {3, 0, 36000}, {2, 1, 33000}, {1, 1, 30000}, {2, 2, 36000}, {4, 0, 42000}} {
		got, err := price(c.adults, c.children)
		if err != nil || got != c.want {
			t.Fatalf("%d adults, %d children: %d %v, want %d", c.adults, c.children, got, err, c.want)
		}
	}
	if _, err := price(3, 2); err != rooms.ErrTooManyGuests {
		t.Fatalf("five guests in a room for four: %v", err)
	}
	if _, err := price(0, 1); err == nil {
		t.Fatal("a room with no adult was priced")
	}
}

func TestPriceNeedsARateForEveryNight(t *testing.T) {
	w, mgr, _ := setup(t)
	rp := plan(t, w, mgr, roomType(t, w, mgr, "Deluxe"))
	mgr.Do("PUT", "/api/rates", &w.A1, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-12-01","to":"2026-12-03","price":10000}`, rp)).Want(t, 200)
	err := db.WithOrg(ctx, w.Pool, w.OrgA, func(tx pgx.Tx) error {
		_, _, e := rooms.Price(ctx, tx, uuid.MustParse(rp), date("2026-12-01"), date("2026-12-04"), 2, 0)
		return e
	})
	if err != rooms.ErrNoRate {
		t.Fatalf("a night with no rate: %v", err)
	}
}

func date(s string) time.Time {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return d
}
