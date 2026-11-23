package alerts_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/alerts"
	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/folio"
	"hotelpms/api/internal/housekeeping"
	"hotelpms/api/internal/rooms"
	"hotelpms/api/internal/testkit"
)

var ctx = testkit.Ctx

func must(t *testing.T, err error) { t.Helper(); testkit.Must(t, err) }

type hotel struct {
	w                      *testkit.World
	owner, mgr, desk, keep *testkit.Person
	prop                   uuid.UUID
	rt, rp                 string
}

func idOf(t *testing.T, r testkit.Reply) string {
	t.Helper()
	var b struct{ ID string }
	must(t, json.Unmarshal([]byte(r.Body), &b))
	return b.ID
}

func newHotel(t *testing.T, nRooms int) *hotel {
	w := testkit.New(t, rooms.Mount, booking.Mount, folio.Mount, housekeeping.Mount, alerts.Mount)
	h := &hotel{w: w, prop: w.A1}
	h.owner = w.Member(w.OrgA, nil, "owner")
	h.mgr = w.Member(w.OrgA, &w.A1, "manager")
	h.desk = w.Member(w.OrgA, &w.A1, "front_desk")
	h.keep = w.Member(w.OrgA, &w.A1, "housekeeping")
	h.rt = idOf(t, h.mgr.Do("POST", "/api/room-types", &h.prop, `{"name":"Deluxe","baseGuests":2,"maxGuests":3}`).Want(t, 201))
	for i := 0; i < nRooms; i++ {
		h.mgr.Do("POST", "/api/rooms", &h.prop, fmt.Sprintf(`{"roomTypeId":%q,"number":"%d","floor":1}`, h.rt, 101+i)).Want(t, 201)
	}
	h.rp = idOf(t, h.mgr.Do("POST", "/api/rate-plans", &h.prop, fmt.Sprintf(`{"roomTypeId":%q,"name":"Flexible"}`, h.rt)).Want(t, 201))
	h.mgr.Do("PUT", "/api/rates", &h.prop, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-11-23","to":"2027-01-01","price":10000}`, h.rp)).Want(t, 200)
	return h
}

func (h *hotel) book(from, to string) string {
	h.w.T.Helper()
	return idOf(h.w.T, h.desk.Do("POST", "/api/reservations", &h.prop, fmt.Sprintf(
		`{"guest":{"name":"Mai Pham","email":"mai@hotel.com"},"rooms":[{"roomTypeId":%q,"ratePlanId":%q,"from":%q,"to":%q,"adults":2,"children":0}]}`, h.rt, h.rp, from, to)).Want(h.w.T, 201))
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

func (h *hotel) assign(res, number string) {
	h.w.T.Helper()
	h.desk.Do("POST", "/api/reservations/"+res+"/assign", &h.prop, fmt.Sprintf(`{"roomId":%q,"force":true,"reason":"test"}`, h.roomID(number))).Want(h.w.T, 204)
}

func (h *hotel) run() {
	h.w.T.Helper()
	must(h.w.T, alerts.Run(ctx, h.w.Pool, h.w.Now))
}

type alert struct {
	ID         string
	CheckKind  string
	Severity   string
	TargetRole string
	Reason     struct{ Text string }
	Action     *struct {
		Kind, Label string
		Changes     []string
	}
}

func (h *hotel) alerts(p *testkit.Person) []alert {
	h.w.T.Helper()
	var out []alert
	must(h.w.T, json.Unmarshal([]byte(p.Do("GET", "/api/alerts", &h.prop, "").Want(h.w.T, 200).Body), &out))
	return out
}

func (h *hotel) of(p *testkit.Person, kind string) []alert {
	h.w.T.Helper()
	var out []alert
	for _, a := range h.alerts(p) {
		if a.CheckKind == kind {
			out = append(out, a)
		}
	}
	return out
}

func (h *hotel) kinds(p *testkit.Person) []string {
	var out []string
	for _, a := range h.alerts(p) {
		out = append(out, a.CheckKind)
	}
	return out
}

func (h *hotel) notifications(p *testkit.Person) int {
	h.w.T.Helper()
	var n int
	must(h.w.T, db.WithOrg(ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select count(*) from notification where user_id = $1 and key like 'alert:%'`, p.ID).Scan(&n)
	}))
	return n
}

func at(h *hotel, hour, minute int) { // moves the clock to a time of day on the 23rd
	h.w.Advance(time.Date(2026, 11, 23, hour, minute, 0, 0, time.UTC).Sub(h.w.Now))
}

func TestAnUnassignedArrivalRaisesOneAlertForTheFrontDeskOnly(t *testing.T) {
	h := newHotel(t, 2)
	res := h.book("2026-11-23", "2026-11-25")
	h.run()
	got := h.of(h.desk, "unassigned_arrival")
	if len(got) != 1 || got[0].CheckKind != "unassigned_arrival" || !strings.Contains(got[0].Reason.Text, "Mai Pham arrives today") || got[0].Action == nil || got[0].Action.Kind != "assign_room" {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(got[0].Action.Changes[0], "room 101") {
		t.Fatalf("the preview does not name the room: %+v", got[0].Action)
	}
	if n := len(h.of(h.mgr, "unassigned_arrival")) + len(h.alerts(h.keep)); n != 0 {
		t.Fatalf("other roles see %d alerts", n)
	}
	if len(h.of(h.owner, "unassigned_arrival")) != 1 {
		t.Fatal("the owner does not see every alert")
	}
	// A trigger that stays true makes no second alert and no second notification.
	h.run()
	h.run()
	if len(h.of(h.desk, "unassigned_arrival")) != 1 || h.notifications(h.desk) != 2 { // one per open alert: no room, and money still to pay
		t.Fatalf("%d alerts, %d notifications", len(h.of(h.desk, "unassigned_arrival")), h.notifications(h.desk))
	}
	// Fixing the cause elsewhere closes it.
	h.assign(res, "101")
	h.run()
	if len(h.of(h.desk, "unassigned_arrival")) != 0 {
		t.Fatal("the alert stayed after the room was assigned")
	}
}

func TestApplyingTheSuggestionDoesWhatThePreviewSaidAndLinksTheTrail(t *testing.T) {
	h := newHotel(t, 2)
	res := h.book("2026-11-23", "2026-11-25")
	h.run()
	a := h.of(h.desk, "unassigned_arrival")[0]
	h.mgr.Do("POST", "/api/alerts/"+a.ID+"/apply", &h.prop, "").Want(t, 404) // not the target role
	h.desk.Do("POST", "/api/alerts/"+a.ID+"/apply", &h.prop, "").Want(t, 204)
	got := h.desk.Do("GET", "/api/reservations/"+res, &h.prop, "").Want(t, 200, `"roomNumber":"101"`)
	_ = got
	h.desk.Do("POST", "/api/alerts/"+a.ID+"/apply", &h.prop, "").Want(t, 409, "already_closed")
	var n int
	must(t, db.WithOrg(ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select count(*) from audit_log where action = 'alert.accepted' and after::text like '%' || $1 || '%'`, a.ID).Scan(&n)
	}))
	if n != 1 {
		t.Fatalf("%d trail entries name the alert", n)
	}
	if len(h.of(h.desk, "unassigned_arrival")) != 0 {
		t.Fatal("an applied alert stays open")
	}
}

func TestARoomThatIsNotReadyAlertsBeforeArrival(t *testing.T) {
	h := newHotel(t, 2)
	res := h.book("2026-11-23", "2026-11-24")
	h.assign(res, "101")
	must(t, db.WithOrg(ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update room set status = 'dirty' where number = '101'`)
		return err
	}))
	at(h, 11, 59)
	h.run()
	if len(h.of(h.desk, "room_not_ready")) != 0 {
		t.Fatal("too early")
	}
	at(h, 12, 0) // two hours before the 14:00 check-in
	h.run()
	got := h.of(h.desk, "room_not_ready")
	if len(got) != 1 || got[0].CheckKind != "room_not_ready" || !strings.Contains(got[0].Reason.Text, "room 101 is dirty") || got[0].Action == nil || !strings.Contains(got[0].Action.Label, "102") {
		t.Fatalf("%+v", got)
	}
	// Cleaning the room closes it.
	must(t, db.WithOrg(ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update room set status = 'clean' where number = '101'`)
		return err
	}))
	h.run()
	if len(h.of(h.desk, "room_not_ready")) != 0 {
		t.Fatal("still open")
	}
}

func TestUnpaidArrivalAndDeparture(t *testing.T) {
	h := newHotel(t, 2)
	res := h.book("2026-11-23", "2026-11-24")
	h.assign(res, "101")
	h.run()
	got := h.kinds(h.desk)
	if len(got) != 1 || got[0] != "unpaid_arrival" {
		t.Fatalf("%v", got)
	}
	if !strings.Contains(h.alerts(h.desk)[0].Reason.Text, "100.00 USD") {
		t.Fatalf("%s", h.alerts(h.desk)[0].Reason.Text)
	}
	f := func() string {
		var v struct{ FolioID string }
		must(t, json.Unmarshal([]byte(h.desk.Do("GET", "/api/reservations/"+res+"/folio", &h.prop, "").Want(t, 200).Body), &v))
		return v.FolioID
	}()
	h.desk.Do("POST", "/api/folios/"+f+"/payments", &h.prop, `{"method":"cash","amount":10000}`).Want(t, 201)
	h.run()
	if h.kinds(h.desk) != nil {
		t.Fatalf("paid in full, yet %v", h.kinds(h.desk))
	}
	h.desk.Do("POST", "/api/reservations/"+res+"/check-in", &h.prop, "").Want(t, 204)
	// Make today the departure day.
	must(t, db.WithOrg(ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update reservation_room set stay = daterange('2026-11-22', '2026-11-23') where reservation_id = $1`, res)
		return err
	}))
	h.desk.Do("POST", "/api/folios/"+f+"/lines", &h.prop, `{"kind":"extra","description":"Minibar","quantity":1,"unitPrice":1500}`).Want(t, 201)
	h.run()
	if got := h.kinds(h.desk); len(got) != 1 || got[0] != "unpaid_departure" {
		t.Fatalf("%v", got)
	}
}

func TestLateCheckoutNeedsAnotherGuestArrivingInTheRoom(t *testing.T) {
	h := newHotel(t, 2)
	leaving := h.book("2026-11-23", "2026-11-24")
	h.assign(leaving, "101")
	// A stay that began yesterday cannot be booked today, so move this one back a day.
	must(t, db.WithOrg(ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update reservation_room set stay = daterange('2026-11-22', '2026-11-23') where reservation_id = $1`, leaving); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `update reservation set status = 'checked_in' where id = $1`, leaving)
		return err
	}))
	at(h, 11, 59)
	h.run()
	if len(h.of(h.desk, "late_checkout")) != 0 {
		t.Fatal("too early")
	}
	at(h, 12, 0)
	h.run()
	if len(h.of(h.desk, "late_checkout")) != 0 {
		t.Fatal("nobody arrives in that room, so a late checkout hurts no one")
	}
	arriving := h.book("2026-11-23", "2026-11-25")
	h.assign(arriving, "101")
	h.run()
	got := h.of(h.desk, "late_checkout")
	if len(got) != 1 || !strings.Contains(got[0].Reason.Text, "room 101") {
		t.Fatalf("%+v", got)
	}
}

func TestALikelyNoShowAppearsAtTenInTheEvening(t *testing.T) {
	h := newHotel(t, 2)
	res := h.book("2026-11-23", "2026-11-24")
	at(h, 21, 59)
	h.run()
	for _, k := range h.kinds(h.desk) {
		if k == "likely_no_show" {
			t.Fatal("too early")
		}
	}
	at(h, 22, 0)
	h.run()
	var a *alert
	for _, x := range h.alerts(h.desk) {
		if x.CheckKind == "likely_no_show" {
			x := x
			a = &x
		}
	}
	if a == nil || a.Action == nil || a.Action.Kind != "open" {
		t.Fatalf("%+v", a)
	}
	at(h, 23, 0)
	h.run()
	for _, x := range h.alerts(h.desk) {
		if x.CheckKind == "likely_no_show" {
			a = &x
		}
	}
	if a.Action.Kind != "no_show" {
		t.Fatalf("after the cut-off: %+v", a.Action)
	}
	h.desk.Do("POST", "/api/alerts/"+a.ID+"/apply", &h.prop, "").Want(t, 204)
	h.desk.Do("GET", "/api/reservations/"+res, &h.prop, "").Want(t, 200, `"status":"no_show"`)
}

func TestAnOutOfOrderClashGoesToTheManager(t *testing.T) {
	h := newHotel(t, 2)
	res := h.book("2026-12-10", "2026-12-12")
	h.assign(res, "101")
	h.keep.Do("POST", "/api/maintenance-tickets", &h.prop, fmt.Sprintf(`{"roomId":%q,"description":"Ceiling","blocksFrom":"2026-12-11","blocksTo":"2026-12-14"}`, h.roomID("101"))).Want(t, 201)
	h.run()
	got := h.alerts(h.mgr)
	if len(got) != 1 || got[0].CheckKind != "out_of_order_clash" || got[0].Severity != "urgent" || !strings.Contains(got[0].Reason.Text, "Room 101") {
		t.Fatalf("%+v", got)
	}
	if len(h.alerts(h.desk)) != 0 {
		t.Fatal("the front desk sees a manager alert")
	}
}

func TestARepeatIssueNeedsThreeTicketsInThirtyDays(t *testing.T) {
	h := newHotel(t, 1)
	for i := 0; i < 2; i++ {
		h.keep.Do("POST", "/api/maintenance-tickets", &h.prop, fmt.Sprintf(`{"roomId":%q,"description":"Tap %d"}`, h.roomID("101"), i)).Want(t, 201)
	}
	h.run()
	if len(h.alerts(h.mgr)) != 0 {
		t.Fatal("two tickets are not a pattern")
	}
	h.keep.Do("POST", "/api/maintenance-tickets", &h.prop, fmt.Sprintf(`{"roomId":%q,"description":"Tap 3"}`, h.roomID("101"))).Want(t, 201)
	h.run()
	if got := h.kinds(h.mgr); len(got) != 1 || got[0] != "repeat_issue" {
		t.Fatalf("%v", got)
	}
}

func TestAReturningGuestIsMentionedAtCheckIn(t *testing.T) {
	h := newHotel(t, 2)
	first := h.book("2026-11-23", "2026-11-24")
	h.assign(first, "101")
	h.desk.Do("POST", "/api/reservations/"+first+"/check-in", &h.prop, "").Want(t, 204)
	h.desk.Do("POST", "/api/reservations/"+first+"/check-out", &h.prop, "").Want(t, 204)
	h.book("2026-11-23", "2026-11-26") // the same guest again
	h.run()
	var found bool
	for _, a := range h.alerts(h.desk) {
		if a.CheckKind == "returning_guest" {
			found = a.Severity == "info" && a.Action == nil && strings.Contains(a.Reason.Text, "stayed before")
		}
	}
	if !found {
		t.Fatalf("%v", h.kinds(h.desk))
	}
}

func TestARefundWaitingGoesToTheManagerAndClosesWhenDecided(t *testing.T) {
	h := newHotel(t, 1)
	res := h.book("2026-12-10", "2026-12-12")
	var v struct{ FolioID string }
	must(t, json.Unmarshal([]byte(h.desk.Do("GET", "/api/reservations/"+res+"/folio", &h.prop, "").Want(t, 200).Body), &v))
	p := idOf(t, h.desk.Do("POST", "/api/folios/"+v.FolioID+"/payments", &h.prop, `{"method":"cash","amount":20000}`).Want(t, 201))
	r := idOf(t, h.desk.Do("POST", "/api/payments/"+p+"/refunds", &h.prop, `{"amount":9000,"reason":"unhappy"}`).Want(t, 201, "pending"))
	h.run()
	if got := h.kinds(h.mgr); len(got) != 1 || got[0] != "refund_waiting" {
		t.Fatalf("%v", got)
	}
	h.mgr.Do("POST", "/api/refunds/"+r+"/approve", &h.prop, "").Want(t, 204)
	h.run()
	if len(h.alerts(h.mgr)) != 0 {
		t.Fatal("the alert stayed after the refund was approved")
	}
}

func TestADismissedAlertStaysAwayForADayWhileTheCauseLasts(t *testing.T) {
	h := newHotel(t, 1)
	res := h.book("2026-12-10", "2026-12-12")
	var v struct{ FolioID string }
	must(t, json.Unmarshal([]byte(h.desk.Do("GET", "/api/reservations/"+res+"/folio", &h.prop, "").Want(t, 200).Body), &v))
	p := idOf(t, h.desk.Do("POST", "/api/folios/"+v.FolioID+"/payments", &h.prop, `{"method":"cash","amount":20000}`).Want(t, 201))
	h.desk.Do("POST", "/api/payments/"+p+"/refunds", &h.prop, `{"amount":9000,"reason":"unhappy"}`).Want(t, 201, "pending") // waits for a manager until someone decides
	h.run()
	a := h.of(h.mgr, "refund_waiting")[0]
	h.mgr.Do("POST", "/api/alerts/"+a.ID+"/dismiss", &h.prop, "").Want(t, 204)
	h.run()
	if len(h.of(h.mgr, "refund_waiting")) != 0 {
		t.Fatal("it came straight back")
	}
	h.w.Advance(23 * time.Hour)
	h.run()
	if len(h.of(h.mgr, "refund_waiting")) != 0 {
		t.Fatal("it came back before 24 hours")
	}
	h.w.Advance(2 * time.Hour)
	h.run()
	if got := h.of(h.mgr, "refund_waiting"); len(got) != 1 || got[0].ID == a.ID {
		t.Fatalf("after a day the unresolved cause should raise a fresh alert: %+v", got)
	}
	h.mgr.Do("POST", "/api/alerts/"+a.ID+"/dismiss", &h.prop, "").Want(t, 409, "already_closed")
}

func TestASnoozedAlertComesBackWhenTheTimeIsUp(t *testing.T) {
	h := newHotel(t, 2)
	h.book("2026-11-23", "2026-11-25")
	h.run()
	a := h.of(h.desk, "unassigned_arrival")[0]
	h.desk.Do("POST", "/api/alerts/"+a.ID+"/snooze", &h.prop, `{"hours":0}`).Want(t, 400)
	h.desk.Do("POST", "/api/alerts/"+a.ID+"/snooze", &h.prop, `{"hours":3}`).Want(t, 204)
	h.run()
	if len(h.of(h.desk, "unassigned_arrival")) != 0 {
		t.Fatal("a snoozed alert is shown")
	}
	h.w.Advance(4 * time.Hour)
	h.run()
	if got := h.of(h.desk, "unassigned_arrival"); len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("%+v", got)
	}
}

func TestTurningACheckOffClosesItsAlertsAndStopsNewOnes(t *testing.T) {
	h := newHotel(t, 2)
	h.book("2026-11-23", "2026-11-25")
	h.run()
	if len(h.of(h.desk, "unassigned_arrival")) != 1 {
		t.Fatal("no alert to turn off")
	}
	h.desk.Do("PUT", "/api/check-settings", &h.prop, `{"checkKind":"unassigned_arrival","enabled":false}`).Want(t, 403)
	h.mgr.Do("PUT", "/api/check-settings", &h.prop, `{"checkKind":"unassigned_arrival","enabled":false}`).Want(t, 204)
	if len(h.of(h.desk, "unassigned_arrival")) != 0 {
		t.Fatal("its alerts stayed open")
	}
	h.run()
	if len(h.of(h.desk, "unassigned_arrival")) != 0 {
		t.Fatal("a switched-off check raised an alert")
	}
	h.mgr.Do("PUT", "/api/check-settings", &h.prop, `{"checkKind":"nonsense","enabled":true}`).Want(t, 400)
	h.mgr.Do("PUT", "/api/check-settings", &h.prop, `{"checkKind":"likely_no_show","enabled":true,"thresholds":{"bogus":1}}`).Want(t, 400)
	h.mgr.Do("PUT", "/api/check-settings", &h.prop, `{"checkKind":"unassigned_arrival","enabled":true}`).Want(t, 204)
	h.run()
	if len(h.of(h.desk, "unassigned_arrival")) != 1 {
		t.Fatal("switching it on again did not bring the alert back")
	}
}

func TestThresholdsCanBeChangedPerProperty(t *testing.T) {
	h := newHotel(t, 2)
	h.book("2026-11-23", "2026-11-24")
	h.mgr.Do("PUT", "/api/check-settings", &h.prop, `{"checkKind":"likely_no_show","enabled":true,"thresholds":{"fromHour":20,"cutoffHour":23}}`).Want(t, 204)
	at(h, 20, 30)
	h.run()
	var found bool
	for _, k := range h.kinds(h.desk) {
		found = found || k == "likely_no_show"
	}
	if !found {
		t.Fatalf("%v", h.kinds(h.desk))
	}
	h.mgr.Do("GET", "/api/check-settings", &h.prop, "").Want(t, 200, `"fromHour":20`, `"checkKind":"returning_guest"`)
}

func TestAnotherPropertyOrOrganizationSeesNothing(t *testing.T) {
	h := newHotel(t, 2)
	h.book("2026-11-23", "2026-11-25")
	h.run()
	a := h.of(h.desk, "unassigned_arrival")[0]
	other := h.w.Member(h.w.OrgB, &h.w.B1, "front_desk")
	other.Do("GET", "/api/alerts", &h.w.B1, "").Want(t, 200, "[]")
	other.Do("POST", "/api/alerts/"+a.ID+"/dismiss", &h.w.B1, "").Want(t, 404)
	desk2 := h.w.Member(h.w.OrgA, &h.w.A2, "front_desk")
	desk2.Do("POST", "/api/alerts/"+a.ID+"/dismiss", &h.w.A2, "").Want(t, 404)
	h.w.Anon("GET", "/api/alerts", "").Want(t, 401)
}

var _ = time.Now

func TestTheStatusSaysWhenTheChecksLastRan(t *testing.T) {
	h := newHotel(t, 1)
	var s struct{ LastRun *time.Time }
	must(t, json.Unmarshal([]byte(h.desk.Do("GET", "/api/alerts/status", &h.prop, "").Want(t, 200).Body), &s))
	if s.LastRun != nil {
		t.Fatalf("the checks never ran, yet %v", s.LastRun)
	}
	h.run()
	must(t, json.Unmarshal([]byte(h.desk.Do("GET", "/api/alerts/status", &h.prop, "").Want(t, 200).Body), &s))
	if s.LastRun == nil || !s.LastRun.Equal(h.w.Now) {
		t.Fatalf("%v, want %v", s.LastRun, h.w.Now)
	}
	h.w.Advance(time.Minute)
	h.run()
	must(t, json.Unmarshal([]byte(h.desk.Do("GET", "/api/alerts/status", &h.prop, "").Want(t, 200).Body), &s))
	if !s.LastRun.Equal(h.w.Now) {
		t.Fatalf("the time did not move: %v", s.LastRun)
	}
	h.w.Anon("GET", "/api/alerts/status", "").Want(t, 401)
}

func TestAmountsInAlertsFollowTheCurrencysDecimals(t *testing.T) {
	h := newHotel(t, 2)
	must(t, db.WithOrg(ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update property set currency = 'VND' where id = $1`, h.w.A1)
		return err
	}))
	res := h.book("2026-11-23", "2026-11-24")
	h.assign(res, "101")
	h.run()
	got := h.of(h.desk, "unpaid_arrival")
	if len(got) != 1 || !strings.Contains(got[0].Reason.Text, "10000 VND") || strings.Contains(got[0].Reason.Text, ".00") {
		t.Fatalf("%+v", got)
	}
}

func TestTheNightlyRunKeepsInventoryRowsAheadOfTheCalendar(t *testing.T) {
	h := newHotel(t, 2)
	count := func(d time.Time) (n int) {
		must(t, db.WithOrg(ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `select count(*) from inventory where room_type_id = $1 and date = $2`, h.rt, d).Scan(&n)
		}))
		return
	}
	h.w.Advance(401 * 24 * time.Hour)
	far := h.w.Now.AddDate(0, 0, 300).Truncate(24 * time.Hour)
	if count(far) != 0 {
		t.Fatal("the rows already reach that far")
	}
	h.run()
	if count(far) != 1 {
		t.Fatal("the run did not add inventory rows for the coming year")
	}
}
