package reports_test

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/folio"
	"hotelpms/api/internal/reports"
	"hotelpms/api/internal/rooms"
	"hotelpms/api/internal/testkit"
)

func idOf(t *testing.T, r testkit.Reply) string {
	t.Helper()
	var b struct{ ID string }
	testkit.Must(t, json.Unmarshal([]byte(r.Body), &b))
	return b.ID
}

func TestReservationsExportAsCSVForPeopleWhoSeeReportsAndNeverRunAsFormulas(t *testing.T) {
	w := testkit.New(t, rooms.Mount, booking.Mount, folio.Mount, reports.Mount)
	owner := w.Member(w.OrgA, nil, "owner")
	desk := w.Member(w.OrgA, &w.A1, "front_desk")
	other := w.Member(w.OrgB, nil, "owner")
	rt := idOf(t, owner.Do("POST", "/api/room-types", &w.A1, `{"name":"Deluxe","baseGuests":2,"maxGuests":3}`).Want(t, 201))
	owner.Do("POST", "/api/rooms", &w.A1, fmt.Sprintf(`{"roomTypeId":%q,"number":"101","floor":1}`, rt)).Want(t, 201)
	rp := idOf(t, owner.Do("POST", "/api/rate-plans", &w.A1, fmt.Sprintf(`{"roomTypeId":%q,"name":"Flexible"}`, rt)).Want(t, 201))
	owner.Do("PUT", "/api/rates", &w.A1, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-11-23","to":"2027-01-01","price":10000}`, rp)).Want(t, 200)
	res := idOf(t, owner.Do("POST", "/api/reservations", &w.A1, fmt.Sprintf(
		`{"guest":{"name":"=HYPERLINK(\"http://evil\",\"x\")","email":"mai@hotel.com"},"rooms":[{"roomTypeId":%q,"ratePlanId":%q,"from":"2026-12-10","to":"2026-12-12","adults":2,"children":0}]}`, rt, rp)).Want(t, 201))

	desk.Do("GET", "/api/reports/reservations.csv?from=2026-12-01&to=2026-12-31", &w.A1, "").Want(t, 403)
	r := owner.Do("GET", "/api/reports/reservations.csv?from=2026-12-01&to=2026-12-31", &w.A1, "").Want(t, 200)
	rows, err := csv.NewReader(strings.NewReader(r.Body)).ReadAll()
	testkit.Must(t, err)
	if len(rows) != 2 || rows[0][0] != "code" {
		t.Fatalf("%q", rows)
	}
	var guest, total string
	for i, h := range rows[0] {
		switch h {
		case "guest":
			guest = rows[1][i]
		case "total":
			total = rows[1][i]
		}
	}
	if !strings.HasPrefix(guest, "'=") || total != "200.00" {
		t.Fatalf("guest %q total %q", guest, total)
	}
	_ = res
	owner.Do("GET", "/api/reports/reservations.csv?from=2027-02-01&to=2027-02-28", &w.A1, "").Want(t, 200) // an empty period still gives the header
	owner.Do("GET", "/api/reports/reservations.csv?from=nonsense", &w.A1, "").Want(t, 400)
	if body := other.Do("GET", "/api/reports/reservations.csv?from=2026-12-01&to=2026-12-31", &w.B1, "").Want(t, 200).Body; strings.Contains(body, "HYPERLINK") {
		t.Fatal("another organization's export holds this hotel's guests")
	}
}
