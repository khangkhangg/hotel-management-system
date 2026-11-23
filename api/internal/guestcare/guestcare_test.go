package guestcare_test

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/alerts"
	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/guestbooking"
	"hotelpms/api/internal/guestcare"
	"hotelpms/api/internal/payments"
	"hotelpms/api/internal/rooms"
	"hotelpms/api/internal/testkit"
)

type hotel struct {
	w         *testkit.World
	prop      uuid.UUID
	mgr, desk *testkit.Person
	rt, rp    string
}

func idOf(t *testing.T, r testkit.Reply) string {
	t.Helper()
	var b struct{ ID string }
	testkit.Must(t, json.Unmarshal([]byte(r.Body), &b))
	return b.ID
}

func newHotel(t *testing.T) *hotel {
	w := testkit.New(t, rooms.Mount, booking.Mount, guestbooking.Mount(payments.NewFake(), uuid.New()), guestcare.Mount)
	h := &hotel{w: w, prop: w.A1}
	h.mgr = w.Member(w.OrgA, &w.A1, "manager")
	h.desk = w.Member(w.OrgA, &w.A1, "front_desk")
	h.rt = idOf(t, h.mgr.Do("POST", "/api/room-types", &h.prop, `{"name":"Deluxe","baseGuests":2,"maxGuests":3}`).Want(t, 201))
	for i := 0; i < 2; i++ {
		h.mgr.Do("POST", "/api/rooms", &h.prop, fmt.Sprintf(`{"roomTypeId":%q,"number":"%d","floor":1}`, h.rt, 101+i)).Want(t, 201)
	}
	h.rp = idOf(t, h.mgr.Do("POST", "/api/rate-plans", &h.prop, fmt.Sprintf(`{"roomTypeId":%q,"name":"Flexible"}`, h.rt)).Want(t, 201))
	h.mgr.Do("PUT", "/api/rates", &h.prop, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-11-23","to":"2027-03-01","price":10000}`, h.rp)).Want(t, 200)
	return h
}

func (h *hotel) book(t *testing.T, email, from, to string) string {
	t.Helper()
	return idOf(t, h.desk.Do("POST", "/api/reservations", &h.prop, fmt.Sprintf(`{"guest":{"name":"Mai Pham","email":%q},"rooms":[{"roomTypeId":%q,"ratePlanId":%q,"from":%q,"to":%q,"adults":2,"children":0}]}`, email, h.rt, h.rp, from, to)).Want(t, 201))
}

func (h *hotel) sql(t *testing.T, q string, args ...any) {
	t.Helper()
	testkit.Must(t, db.WithOrg(testkit.Ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error { _, err := tx.Exec(testkit.Ctx, q, args...); return err }))
}

func (h *hotel) row(t *testing.T, q string, args ...any) (out string) {
	t.Helper()
	testkit.Must(t, db.WithOrg(testkit.Ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error { return tx.QueryRow(testkit.Ctx, q, args...).Scan(&out) }))
	return out
}

// riskyStay books a stay for a guest who cancelled twice before, from a booking site, with nothing paid: a high cancel risk.
func (h *hotel) riskyStay(t *testing.T) string {
	t.Helper()
	for i := 0; i < 2; i++ {
		id := h.book(t, "risky@hotel.com", "2026-12-20", "2026-12-21")
		h.desk.Do("POST", "/api/reservations/"+id+"/cancel", &h.prop, "").Want(t, 204)
	}
	id := h.book(t, "risky@hotel.com", "2026-11-25", "2026-11-27")
	h.sql(t, `update reservation set source_channel = 'expedia' where id = $1`, uuid.MustParse(id))
	return id
}

var link = regexp.MustCompile(`https://hotel\.test/reconfirm/[0-9a-f-]+/[0-9a-f]+`)

func TestAGuestAtHighRiskIsAskedToReconfirmOnceAndTheLinkWorks(t *testing.T) {
	h := newHotel(t)
	res := h.riskyStay(t)
	h.book(t, "safe@hotel.com", "2026-11-25", "2026-11-26") // low risk, so no message
	n, err := guestcare.RunReconfirm(testkit.Ctx, h.w.Pool, h.w.Mail, "https://hotel.test", h.w.Now)
	testkit.Must(t, err)
	sent := h.w.Mail.Sent()
	if n != 1 || len(sent) != 1 || sent[0].To != "risky@hotel.com" {
		t.Fatalf("%d asked, mail %+v", n, sent)
	}
	l := link.FindString(sent[0].Text)
	if l == "" {
		t.Fatalf("no link in %q", sent[0].Text)
	}
	if n, _ := guestcare.RunReconfirm(testkit.Ctx, h.w.Pool, h.w.Mail, "https://hotel.test", h.w.Now.Add(time.Hour)); n != 0 || len(h.w.Mail.Sent()) != 1 {
		t.Fatalf("the guest was asked twice: %d, %d messages", n, len(h.w.Mail.Sent()))
	}
	path := strings.TrimPrefix(l, "https://hotel.test")
	h.w.Anon("POST", "/api"+path, "").Want(t, 204)
	h.w.Anon("POST", "/api"+path, "").Want(t, 204) // pressing it twice is fine
	if got := h.row(t, `select (confirmed_at is not null)::text from reconfirm where reservation_id = $1`, uuid.MustParse(res)); got != "true" {
		t.Fatalf("not confirmed: %s", got)
	}
	h.w.Anon("POST", "/api"+path[:len(path)-2]+"00", "").Want(t, 404)
	h.w.Anon("POST", "/api/reconfirm/nope/nope", "").Want(t, 404)
}

func TestAnAlertAsksTheFrontDeskToCallWhenAGuestDoesNotReconfirm(t *testing.T) {
	h := newHotel(t)
	h.riskyStay(t)
	_, err := guestcare.RunReconfirm(testkit.Ctx, h.w.Pool, h.w.Mail, "https://hotel.test", h.w.Now)
	testkit.Must(t, err)
	open := func(at time.Time) string {
		testkit.Must(t, alerts.Run(testkit.Ctx, h.w.Pool, at))
		return h.row(t, `select count(*)::text from alert where check_kind = 'reconfirm_missing' and status = 'open'`)
	}
	if n := open(h.w.Now.Add(2 * time.Hour)); n != "0" {
		t.Fatalf("an alert after 2 hours: %s", n)
	}
	if n := open(h.w.Now.Add(25 * time.Hour)); n != "1" {
		t.Fatalf("%s alerts after a day without an answer", n)
	}
}

func TestAWaitingGuestIsToldOnceWhenARoomFreesUp(t *testing.T) {
	h := newHotel(t)
	a := h.book(t, "a@hotel.com", "2026-12-10", "2026-12-12")
	h.book(t, "b@hotel.com", "2026-12-10", "2026-12-12") // both rooms taken
	body := `{"name":"Lan Vo","email":"lan@hotel.com","arrive":"2026-12-10","depart":"2026-12-12","adults":2,"locale":"en"}`
	h.w.Anon("POST", "/api/public/a1/waitlist", body).Want(t, 204)
	h.w.Anon("POST", "/api/public/a1/waitlist", body).Want(t, 204) // the same request again changes nothing
	for _, bad := range []string{`{"name":"","email":"lan@hotel.com","arrive":"2026-12-10","depart":"2026-12-12","adults":2}`, `{"name":"Lan","email":"nope","arrive":"2026-12-10","depart":"2026-12-12","adults":2}`, `{"name":"Lan","email":"lan@hotel.com","arrive":"2026-12-12","depart":"2026-12-10","adults":2}`, `{"name":"Lan","email":"lan@hotel.com","arrive":"2020-01-01","depart":"2020-01-03","adults":2}`} {
		h.w.Anon("POST", "/api/public/a1/waitlist", bad).Want(t, 400)
	}
	h.desk.Do("GET", "/api/waitlist", &h.prop, "").Want(t, 200, "Lan Vo", "2026-12-10")
	h.w.Member(h.w.OrgA, &h.w.A1, "housekeeping").Do("GET", "/api/waitlist", &h.prop, "").Want(t, 403)
	if n, err := guestcare.NotifyWaitlist(testkit.Ctx, h.w.Pool, h.w.Mail, "https://hotel.test", h.w.Now); err != nil || n != 0 || len(h.w.Mail.Sent()) != 0 {
		t.Fatalf("told while full: %d %v %v", n, err, h.w.Mail.Sent())
	}
	h.desk.Do("POST", "/api/reservations/"+a+"/cancel", &h.prop, "").Want(t, 204)
	n, err := guestcare.NotifyWaitlist(testkit.Ctx, h.w.Pool, h.w.Mail, "https://hotel.test", h.w.Now)
	sent := h.w.Mail.Sent()
	if err != nil || n != 1 || len(sent) != 1 || sent[0].To != "lan@hotel.com" || !strings.Contains(sent[0].Text, "https://hotel.test/book/a1") {
		t.Fatalf("%d %v %+v", n, err, sent)
	}
	if n, _ := guestcare.NotifyWaitlist(testkit.Ctx, h.w.Pool, h.w.Mail, "https://hotel.test", h.w.Now); n != 0 || len(h.w.Mail.Sent()) != 1 {
		t.Fatal("the guest was told twice")
	}
	h.desk.Do("GET", "/api/waitlist", &h.prop, "").Want(t, 200, `"notified":true`)
}
