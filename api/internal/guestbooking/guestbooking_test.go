package guestbooking_test

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
	"hotelpms/api/internal/folio"
	"hotelpms/api/internal/guestbooking"
	"hotelpms/api/internal/payments"
	"hotelpms/api/internal/rooms"
	"hotelpms/api/internal/testkit"
)

type hotel struct {
	w        *testkit.World
	owner    *testkit.Person
	mgr      *testkit.Person
	prop     *uuid.UUID
	fake     *payments.Fake
	rt, rp   string
	roomsOut int
}

func idOf(t *testing.T, r testkit.Reply) string {
	t.Helper()
	var b struct{ ID string }
	testkit.Must(t, json.Unmarshal([]byte(r.Body), &b))
	return b.ID
}

// newHotel opens a property at /a1 with one Deluxe room type, nRooms rooms, one rate plan at 100.00 and every payment type on.
func newHotel(t *testing.T, nRooms int) *hotel {
	t.Helper()
	fake := payments.NewFake()
	w := testkit.New(t, rooms.Mount, booking.Mount, folio.Mount, guestbooking.Mount(fake))
	h := &hotel{w: w, fake: fake, prop: &w.A1}
	h.owner = w.Member(w.OrgA, nil, "owner")
	h.mgr = w.Member(w.OrgA, &w.A1, "manager")
	h.rt = idOf(t, h.mgr.Do("POST", "/api/room-types", h.prop, `{"name":"Deluxe","baseGuests":2,"maxGuests":3}`).Want(t, 201))
	for i := 0; i < nRooms; i++ {
		h.mgr.Do("POST", "/api/rooms", h.prop, fmt.Sprintf(`{"roomTypeId":%q,"number":"%d","floor":1}`, h.rt, 101+i)).Want(t, 201)
	}
	h.rp = idOf(t, h.mgr.Do("POST", "/api/rate-plans", h.prop, fmt.Sprintf(`{"roomTypeId":%q,"name":"Flexible"}`, h.rt)).Want(t, 201))
	h.mgr.Do("PUT", "/api/rates", h.prop, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-11-23","to":"2027-01-01","price":10000}`, h.rp)).Want(t, 200)
	for _, k := range []string{"hotel", "bank_transfer", "card"} {
		h.owner.Do("PUT", "/api/payment-types/"+k, h.prop, `{"enabled":true,"holdHours":24,"bankDetails":"IBAN 123"}`).Want(t, 204)
	}
	return h
}

func (h *hotel) booking(kind string, from, to string) string {
	return fmt.Sprintf(`{"guest":{"name":"Mai Pham","email":"mai@hotel.com","phone":"+84 90 111 2222","locale":"en"},"paymentKind":%q,"rooms":[{"roomTypeId":%q,"ratePlanId":%q,"from":%q,"to":%q,"adults":2,"children":0}]}`, kind, h.rt, h.rp, from, to)
}

type made struct {
	Code        string
	Total       int64
	Status      string
	CheckoutURL string `json:"checkoutUrl"`
	BankDetails string `json:"bankDetails"`
	HoldUntil   string `json:"holdUntil"`
}

func (h *hotel) book(t *testing.T, kind, from, to string) made {
	t.Helper()
	var m made
	r := h.w.Anon("POST", "/api/public/a1/bookings", h.booking(kind, from, to)).Want(t, 201)
	testkit.Must(t, json.Unmarshal([]byte(r.Body), &m))
	return m
}

func (h *hotel) row(t *testing.T, q string, args ...any) (out string) {
	t.Helper()
	testkit.Must(t, db.WithOrg(testkit.Ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
		return tx.QueryRow(testkit.Ctx, q, args...).Scan(&out)
	}))
	return out
}

func (h *hotel) hook(t *testing.T, id, typ, code string, amount int64) testkit.Reply {
	t.Helper()
	body := fmt.Sprintf(`{"id":%q,"type":%q,"reference":"%s:%s","paymentRef":"pi_1","amount":%d,"currency":"USD"}`, id, typ, h.w.OrgA, code, amount)
	return h.w.AnonWith("POST", "/api/webhooks/fake", body, map[string]string{"X-Fake-Signature": "ok"})
}

func TestOnlyTheOwnerSetsPaymentTypesAndOneMustStayOn(t *testing.T) {
	h := newHotel(t, 1)
	h.mgr.Do("PUT", "/api/payment-types/card", h.prop, `{"enabled":false,"holdHours":24}`).Want(t, 403)
	h.owner.Do("PUT", "/api/payment-types/card", h.prop, `{"enabled":false,"holdHours":24}`).Want(t, 204)
	h.owner.Do("PUT", "/api/payment-types/bank_transfer", h.prop, `{"enabled":false,"holdHours":24}`).Want(t, 204)
	h.owner.Do("PUT", "/api/payment-types/hotel", h.prop, `{"enabled":false,"holdHours":24}`).Want(t, 409, "need_one")
	h.owner.Do("PUT", "/api/payment-types/cheque", h.prop, `{"enabled":true,"holdHours":24}`).Want(t, 400)
	h.owner.Do("PUT", "/api/payment-types/card", h.prop, `{"enabled":true,"holdHours":0}`).Want(t, 400)
}

func TestThePublicPageListsOnlyTheEnabledPaymentTypes(t *testing.T) {
	h := newHotel(t, 1)
	h.owner.Do("PUT", "/api/payment-types/bank_transfer", h.prop, `{"enabled":false,"holdHours":24}`).Want(t, 204)
	r := h.w.Anon("GET", "/api/public/a1", "").Want(t, 200, `"currency":"USD"`, `"card"`, `"hotel"`)
	if strings.Contains(r.Body, "bank_transfer") || strings.Contains(r.Body, "IBAN") {
		t.Fatalf("%s", r.Body)
	}
	h.w.Anon("GET", "/api/public/nowhere", "").Want(t, 404)
}

func TestTheQuoteIsExactlyWhatTheBookingCharges(t *testing.T) {
	h := newHotel(t, 1)
	h.owner.Do("POST", "/api/tax-rules", h.prop, `{"name":"VAT","percent":10,"inclusive":false}`).Want(t, 201)
	var q struct {
		Total   int64
		SoldOut bool `json:"soldOut"`
	}
	r := h.w.Anon("POST", "/api/public/a1/quote", h.booking("card", "2026-12-10", "2026-12-12")).Want(t, 200)
	testkit.Must(t, json.Unmarshal([]byte(r.Body), &q))
	if q.Total != 22000 || q.SoldOut {
		t.Fatalf("quote %+v", q)
	}
	// Asking for a quote holds nothing.
	if got := h.row(t, `select coalesce(sum(sold),0)::text from inventory`); got != "0" {
		t.Fatalf("a quote sold %s nights", got)
	}
	m := h.book(t, "card", "2026-12-10", "2026-12-12")
	if m.Total != q.Total {
		t.Fatalf("booking %d, quote %d", m.Total, q.Total)
	}
}

func TestACardBookingHoldsTheRoomAndIsConfirmedByTheWebhookOnce(t *testing.T) {
	h := newHotel(t, 1)
	m := h.book(t, "card", "2026-12-10", "2026-12-12")
	if m.Status != "pending" || !strings.HasPrefix(m.CheckoutURL, "https://pay.fake.test/") || m.HoldUntil == "" {
		t.Fatalf("%+v", m)
	}
	if got := h.row(t, `select sold::text from inventory where date = '2026-12-10'`); got != "1" {
		t.Fatalf("sold %s", got)
	}
	// The webhook may arrive before the guest returns, and may arrive twice.
	h.hook(t, "evt_1", "paid", m.Code, m.Total).Want(t, 204)
	h.hook(t, "evt_1", "paid", m.Code, m.Total).Want(t, 204)
	if got := h.row(t, `select status from reservation where code = $1`, m.Code); got != "confirmed" {
		t.Fatalf("status %s", got)
	}
	if got := h.row(t, `select count(*)::text from payment where provider_ref = 'pi_1'`); got != "1" {
		t.Fatalf("%s payments", got)
	}
	testkit.Must(t, db.WithOrg(testkit.Ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
		var id, prop uuid.UUID
		if err := tx.QueryRow(testkit.Ctx, `select id, property_id from reservation where code = $1`, m.Code).Scan(&id, &prop); err != nil {
			return err
		}
		bal, _, err := folio.Balance(testkit.Ctx, tx, h.w.OrgA, prop, id, h.w.Now)
		if bal != 0 {
			t.Fatalf("balance %d", bal)
		}
		return err
	}))
}

func TestAForgedWebhookIsRefusedAndRecordsNothing(t *testing.T) {
	h := newHotel(t, 1)
	m := h.book(t, "card", "2026-12-10", "2026-12-12")
	body := fmt.Sprintf(`{"id":"evt_x","type":"paid","reference":"%s:%s","paymentRef":"pi_9","amount":%d,"currency":"USD"}`, h.w.OrgA, m.Code, m.Total)
	h.w.Anon("POST", "/api/webhooks/fake", body).Want(t, 400)
	h.w.Anon("POST", "/api/webhooks/nobody", body).Want(t, 404)
	if got := h.row(t, `select count(*)::text from payment`); got != "0" {
		t.Fatalf("%s payments", got)
	}
}

func TestAnUnpaidHoldIsReleasedAndALatePaymentIsFlagged(t *testing.T) {
	h := newHotel(t, 1)
	m := h.book(t, "card", "2026-12-10", "2026-12-12")
	h.w.Advance(10 * time.Minute)
	n, err := guestbooking.ExpireHolds(testkit.Ctx, h.w.Pool, h.w.Now)
	testkit.Must(t, err)
	if n != 0 {
		t.Fatalf("released %d holds inside the 15 minutes", n)
	}
	h.w.Advance(6 * time.Minute)
	n, err = guestbooking.ExpireHolds(testkit.Ctx, h.w.Pool, h.w.Now)
	testkit.Must(t, err)
	if n != 1 {
		t.Fatalf("released %d", n)
	}
	if got := h.row(t, `select status from reservation where code = $1`, m.Code); got != "cancelled" {
		t.Fatalf("status %s", got)
	}
	if got := h.row(t, `select coalesce(sum(sold),0)::text from inventory`); got != "0" {
		t.Fatalf("sold %s", got)
	}
	// The guest paid after the hold ran out: the money is recorded, the booking stays cancelled, and staff are told.
	h.hook(t, "evt_late", "paid", m.Code, m.Total).Want(t, 204)
	if got := h.row(t, `select paid_late::text from reservation where code = $1`, m.Code); got != "true" {
		t.Fatalf("paid_late %s", got)
	}
	if got := h.row(t, `select status from reservation where code = $1`, m.Code); got != "cancelled" {
		t.Fatalf("status %s", got)
	}
	if got := h.row(t, `select count(*)::text from payment where provider_ref = 'pi_1'`); got != "1" {
		t.Fatalf("%s payments", got)
	}
}

func TestTwoGuestsBookingTheLastRoomOneIsTurnedAway(t *testing.T) {
	h := newHotel(t, 1)
	h.book(t, "card", "2026-12-10", "2026-12-12")
	h.w.Anon("POST", "/api/public/a1/bookings", h.booking("card", "2026-12-11", "2026-12-13")).Want(t, 409, "sold_out")
	var q struct {
		SoldOut bool `json:"soldOut"`
	}
	r := h.w.Anon("POST", "/api/public/a1/quote", h.booking("card", "2026-12-11", "2026-12-13")).Want(t, 200)
	testkit.Must(t, json.Unmarshal([]byte(r.Body), &q))
	if !q.SoldOut {
		t.Fatal("quote says there is room")
	}
}

func TestATurnedOffPaymentTypeCannotBeUsed(t *testing.T) {
	h := newHotel(t, 1)
	h.owner.Do("PUT", "/api/payment-types/bank_transfer", h.prop, `{"enabled":false,"holdHours":24}`).Want(t, 204)
	h.w.Anon("POST", "/api/public/a1/bookings", h.booking("bank_transfer", "2026-12-10", "2026-12-12")).Want(t, 409, "payment_type_off")
	h.w.Anon("POST", "/api/public/a1/bookings", h.booking("cheque", "2026-12-10", "2026-12-12")).Want(t, 400)
}

func TestAPayAtTheHotelBookingIsConfirmedAtOnceAndABankTransferWaitsItsHours(t *testing.T) {
	h := newHotel(t, 2)
	a := h.book(t, "hotel", "2026-12-10", "2026-12-12")
	if a.Status != "confirmed" || a.CheckoutURL != "" {
		t.Fatalf("%+v", a)
	}
	b := h.book(t, "bank_transfer", "2026-12-10", "2026-12-12")
	if b.Status != "pending" || b.BankDetails != "IBAN 123" {
		t.Fatalf("%+v", b)
	}
	h.w.Advance(23 * time.Hour)
	n, err := guestbooking.ExpireHolds(testkit.Ctx, h.w.Pool, h.w.Now)
	testkit.Must(t, err)
	if n != 0 {
		t.Fatalf("a transfer was released after 23 of 24 hours")
	}
	h.w.Advance(2 * time.Hour)
	n, err = guestbooking.ExpireHolds(testkit.Ctx, h.w.Pool, h.w.Now)
	testkit.Must(t, err)
	if n != 1 || h.row(t, `select status from reservation where code = $1`, a.Code) != "confirmed" {
		t.Fatalf("released %d, or the pay-at-hotel booking was touched", n)
	}
}

func TestNoCardDataIsKeptAnywhere(t *testing.T) {
	h := newHotel(t, 1)
	body := strings.Replace(h.booking("card", "2026-12-10", "2026-12-12"), `"paymentKind"`, `"cardNumber":"4242424242424242","paymentKind"`, 1)
	h.w.Anon("POST", "/api/public/a1/bookings", body).Want(t, 400)
}
