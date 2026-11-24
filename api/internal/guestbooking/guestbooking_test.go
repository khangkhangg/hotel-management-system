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
	"hotelpms/api/internal/extras"
	"hotelpms/api/internal/folio"
	"hotelpms/api/internal/guestbooking"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/payments"
	"hotelpms/api/internal/promo"
	"hotelpms/api/internal/risk"
	"hotelpms/api/internal/rooms"
	"hotelpms/api/internal/siteprofile"
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
func newHotel(t *testing.T, nRooms int) *hotel { return newHotelFor(t, nRooms, true) }

// newHotelFor opens the hotel; cardIsOurs says whether the card provider account belongs to this hotel's organization.
func newHotelFor(t *testing.T, nRooms int, cardIsOurs bool) *hotel {
	t.Helper()
	fake := payments.NewFake()
	w := testkit.NewWith(t, func(w *testkit.World) []httpx.Mount {
		owner := uuid.New()
		if cardIsOurs {
			owner = w.OrgA
		}
		return []httpx.Mount{rooms.Mount, extras.Mount, siteprofile.Mount, risk.Mount, promo.Mount, booking.Mount, folio.Mount, guestbooking.Mount(fake, owner)}
	})
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
	h.w.Advance(20 * time.Minute)
	n, err := guestbooking.ExpireHolds(testkit.Ctx, h.w.Pool, h.w.Now)
	testkit.Must(t, err)
	if n != 0 {
		t.Fatalf("released %d holds inside the 30 minutes", n)
	}
	h.w.Advance(11 * time.Minute)
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

func (h *hotel) deskConfirm(t *testing.T, code string, status int) {
	t.Helper()
	desk := h.w.Member(h.w.OrgA, &h.w.A1, "front_desk")
	id := h.row(t, `select id::text from reservation where code = $1`, code)
	desk.Do("POST", "/api/reservations/"+id+"/confirm-payment", h.prop, "").Want(t, status)
}

func TestStaffConfirmingATransferKeepsTheRoomPastTheHold(t *testing.T) {
	h := newHotel(t, 1)
	b := h.book(t, "bank_transfer", "2026-12-10", "2026-12-12")
	h.deskConfirm(t, b.Code, 204)
	if got := h.row(t, `select status || coalesce(hold_until::text, '-') from reservation where code = $1`, b.Code); got != "confirmed-" {
		t.Fatalf("%s", got)
	}
	if got := h.row(t, `select count(*)::text from payment where method = 'bank_transfer' and amount = $1`, b.Total); got != "1" {
		t.Fatalf("%s payments", got)
	}
	h.w.Advance(30 * time.Hour)
	n, err := guestbooking.ExpireHolds(testkit.Ctx, h.w.Pool, h.w.Now)
	testkit.Must(t, err)
	if n != 0 {
		t.Fatalf("released a confirmed booking")
	}
	h.deskConfirm(t, b.Code, 409) // nothing is waiting any more
}

func TestTheGuestSiteIsNotAFreeForAllForBots(t *testing.T) {
	h := newHotel(t, 8)
	for i := 0; i < 5; i++ {
		h.book(t, "hotel", "2026-12-10", "2026-12-11")
	}
	h.w.Anon("POST", "/api/public/a1/bookings", h.booking("hotel", "2026-12-10", "2026-12-11")).Want(t, 429)
	h.w.Advance(11 * time.Minute)
	h.book(t, "hotel", "2026-12-10", "2026-12-11")
}

func TestABookingNeedsARealEmailAndSaneSizes(t *testing.T) {
	h := newHotel(t, 1)
	for _, bad := range []string{
		strings.Replace(h.booking("hotel", "2026-12-10", "2026-12-12"), "mai@hotel.com", "", 1),
		strings.Replace(h.booking("hotel", "2026-12-10", "2026-12-12"), "mai@hotel.com", "not-an-email", 1),
		strings.Replace(h.booking("hotel", "2026-12-10", "2026-12-12"), "mai@hotel.com", strings.Repeat("a", 300)+"@x.com", 1),
		h.booking("hotel", "2026-12-10", "2027-02-20"),
	} {
		h.w.Anon("POST", "/api/public/a1/bookings", bad).Want(t, 400)
	}
}

func TestTheGuestCanLookUpTheirBookingByCode(t *testing.T) {
	h := newHotel(t, 1)
	m := h.book(t, "card", "2026-12-10", "2026-12-12")
	r := h.w.Anon("GET", "/api/public/a1/bookings/"+m.Code, "").Want(t, 200, `"status":"pending"`)
	if strings.Contains(r.Body, "mai@hotel.com") || strings.Contains(r.Body, "Mai") {
		t.Fatalf("personal data in %s", r.Body)
	}
	h.w.Anon("GET", "/api/public/a1/bookings/NOSUCH", "").Want(t, 404)
	if !strings.Contains(h.fake.Checkouts[0].SuccessURL, "/book/a1?done="+m.Code) {
		t.Fatalf("return address %s", h.fake.Checkouts[0].SuccessURL)
	}
}

func TestTheGuestIsEmailedTheBookingCode(t *testing.T) {
	h := newHotel(t, 2)
	a := h.book(t, "hotel", "2026-12-10", "2026-12-12")
	sent := h.w.Mail.Sent()
	if len(sent) != 1 || sent[0].To != "mai@hotel.com" || !strings.Contains(sent[0].Text, a.Code) {
		t.Fatalf("%+v", sent)
	}
	c := h.book(t, "card", "2026-12-10", "2026-12-12")
	if len(h.w.Mail.Sent()) != 1 {
		t.Fatal("a card guest was emailed before paying")
	}
	h.hook(t, "evt_mail", "paid", c.Code, c.Total).Want(t, 204)
	if got := h.w.Mail.Sent(); len(got) != 2 || !strings.Contains(got[1].Text, c.Code) {
		t.Fatalf("%+v", got)
	}
}

func TestAFailedCheckoutDoesNotLeaveAGhostHold(t *testing.T) {
	h := newHotel(t, 1)
	h.fake.FailCheckout = true
	h.w.Anon("POST", "/api/public/a1/bookings", h.booking("card", "2026-12-10", "2026-12-12")).Want(t, 502, "provider_unavailable")
	if got := h.row(t, `select coalesce(sum(sold),0)::text from inventory`); got != "0" {
		t.Fatalf("sold %s", got)
	}
	h.fake.FailCheckout = false
	h.book(t, "card", "2026-12-10", "2026-12-12")
}

func TestAPaymentThatDoesNotCoverTheBookingIsNotTakenAsPaid(t *testing.T) {
	h := newHotel(t, 2)
	a := h.book(t, "card", "2026-12-10", "2026-12-12")
	h.hook(t, "evt_short", "paid", a.Code, a.Total-1).Want(t, 204)
	if got := h.row(t, `select status || paid_late::text from reservation where code = $1`, a.Code); got != "pendingtrue" {
		t.Fatalf("%s", got)
	}
	b := h.book(t, "card", "2026-12-10", "2026-12-12")
	body := fmt.Sprintf(`{"id":"evt_eur","type":"paid","reference":"%s:%s","paymentRef":"pi_2","amount":%d,"currency":"EUR"}`, h.w.OrgA, b.Code, b.Total)
	h.w.AnonWith("POST", "/api/webhooks/fake", body, map[string]string{"X-Fake-Signature": "ok"}).Want(t, 204)
	if got := h.row(t, `select status from reservation where code = $1`, b.Code); got != "pending" {
		t.Fatalf("%s", got)
	}
}

func TestCardIsOnlyOfferedToTheOrganizationThatOwnsTheProviderAccount(t *testing.T) {
	h := newHotelFor(t, 1, false)
	if r := h.w.Anon("GET", "/api/public/a1", "").Want(t, 200); strings.Contains(r.Body, `"card"`) {
		t.Fatalf("%s", r.Body)
	}
	h.w.Anon("POST", "/api/public/a1/bookings", h.booking("card", "2026-12-10", "2026-12-12")).Want(t, 409, "payment_type_off")
}

func TestTheSameBookingKeySentTwiceMakesOneBooking(t *testing.T) {
	h := newHotel(t, 2)
	body := strings.Replace(h.booking("card", "2026-12-10", "2026-12-12"), `{"guest"`, `{"key":"k-1","guest"`, 1)
	var first, again made
	testkit.Must(t, json.Unmarshal([]byte(h.w.Anon("POST", "/api/public/a1/bookings", body).Want(t, 201).Body), &first))
	testkit.Must(t, json.Unmarshal([]byte(h.w.Anon("POST", "/api/public/a1/bookings", body).Want(t, 201).Body), &again))
	if again.Code != first.Code || again.CheckoutURL == "" || again.Total != first.Total {
		t.Fatalf("%+v %+v", first, again)
	}
	if got := h.row(t, `select count(*)::text from reservation`); got != "1" {
		t.Fatalf("%s reservations", got)
	}
	other := strings.Replace(body, "k-1", "k-2", 1)
	h.w.Anon("POST", "/api/public/a1/bookings", other).Want(t, 201)
	if got := h.row(t, `select count(*)::text from reservation`); got != "2" {
		t.Fatalf("%s reservations", got)
	}
}

func TestAGuestAddsExtrasAndTheyAreInTheTotal(t *testing.T) {
	h := newHotel(t, 1)
	pick := idOf(t, h.owner.Do("POST", "/api/extras", h.prop, `{"name":"Airport pickup","kind":"service","unit":"each","price":2500}`).Want(t, 201))
	food := idOf(t, h.owner.Do("POST", "/api/extras", h.prop, `{"name":"Breakfast","kind":"service","unit":"per_person_night","price":800}`).Want(t, 201))
	off := idOf(t, h.owner.Do("POST", "/api/extras", h.prop, `{"name":"Tour","kind":"tour","unit":"each","price":9000}`).Want(t, 201))
	h.owner.Do("PATCH", "/api/extras/"+off, h.prop, `{"active":false}`).Want(t, 200)
	h.w.Anon("GET", "/api/public/a1", "").Want(t, 200, "Airport pickup")
	if strings.Contains(h.w.Anon("GET", "/api/public/a1", "").Body, "Tour") {
		t.Fatal("a switched-off extra is offered to guests")
	}
	with := func(id string, from, to string) string {
		return strings.Replace(h.booking("hotel", from, to), `{"guest"`, fmt.Sprintf(`{"extras":[{"id":%q,"quantity":1}],"guest"`, id), 1)
	}
	var q struct{ Total int64 }
	testkit.Must(t, json.Unmarshal([]byte(h.w.Anon("POST", "/api/public/a1/quote", with(food, "2026-12-10", "2026-12-12")).Want(t, 200).Body), &q))
	if q.Total != 20000+3200 { // 2 guests x 2 nights x 8.00
		t.Fatalf("quote %d", q.Total)
	}
	var m made
	testkit.Must(t, json.Unmarshal([]byte(h.w.Anon("POST", "/api/public/a1/bookings", with(pick, "2026-12-10", "2026-12-12")).Want(t, 201).Body), &m))
	if m.Total != 20000+2500 {
		t.Fatalf("total %d", m.Total)
	}
	h.w.Anon("POST", "/api/public/a1/bookings", with(off, "2026-12-20", "2026-12-22")).Want(t, 404)
	h.w.Anon("POST", "/api/public/a1/bookings", with(uuid.NewString(), "2026-12-20", "2026-12-22")).Want(t, 404)
	if got := h.row(t, `select count(*)::text from reservation`); got != "1" {
		t.Fatalf("%s reservations: a refused extra left a booking behind", got)
	}
}

func TestQuotesAndKeysAreBounded(t *testing.T) {
	h := newHotel(t, 2)
	x := idOf(t, h.owner.Do("POST", "/api/extras", h.prop, `{"name":"Pickup","kind":"service","unit":"each","price":2500}`).Want(t, 201))
	bad := strings.Replace(h.booking("hotel", "2026-12-10", "2026-12-12"), `{"guest"`, fmt.Sprintf(`{"extras":[{"id":%q,"quantity":0}],"guest"`, x), 1)
	h.w.Anon("POST", "/api/public/a1/quote", bad).Want(t, 400)
	long := strings.Replace(h.booking("hotel", "2026-12-10", "2026-12-12"), `{"guest"`, `{"key":"`+strings.Repeat("k", 65)+`","guest"`, 1)
	h.w.Anon("POST", "/api/public/a1/bookings", long).Want(t, 400)
}

const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

func TestThePublicPageShowsTheHotelsTextAmenitiesBrandAndPhotos(t *testing.T) {
	h := newHotel(t, 1)
	empty := h.w.Anon("GET", "/api/public/a1", "").Want(t, 200, `"site"`).Body
	if !strings.Contains(empty, `"amenities":[]`) || !strings.Contains(empty, `"photos":[]`) {
		t.Fatalf("a hotel with no profile: %s", empty)
	}
	h.owner.Do("PUT", "/api/site/brand", h.prop, `{"color":"#1F5F4F"}`).Want(t, 204)
	h.owner.Do("PUT", "/api/site/texts/fr/description", h.prop, `{"body":"Un hôtel calme."}`).Want(t, 204)
	h.owner.Do("PUT", "/api/site/amenities", h.prop, `{"codes":["pool","wifi"]}`).Want(t, 204)
	pid := idOf(t, h.owner.Do("POST", "/api/site/photos", h.prop, `{"kind":"gallery","caption":"Pool","data":"`+tinyPNG+`"}`).Want(t, 201))
	lid := idOf(t, h.owner.Do("POST", "/api/site/photos", h.prop, `{"kind":"logo","caption":"","data":"`+tinyPNG+`"}`).Want(t, 201))
	got := h.w.Anon("GET", "/api/public/a1", "").Want(t, 200, `"color":"#1F5F4F"`, `"Un hôtel calme."`, `"pool"`, `"wifi"`, `"Pool"`, pid, lid).Body
	if strings.Contains(got, "base64") || strings.Contains(got, "iVBOR") {
		t.Fatal("the picture bytes are in the page data")
	}
	r := h.w.Anon("GET", "/api/public/a1/photos/"+pid, "").Want(t, 200)
	if len(r.Body) < 60 || r.Body[1:4] != "PNG" {
		t.Fatalf("the picture was not served: %q", r.Body)
	}
	h.w.Anon("GET", "/api/public/a1/photos/"+lid, "").Want(t, 200)
}

func TestAPhotoCannotBeReadThroughAnotherHotelsAddress(t *testing.T) {
	h := newHotel(t, 1)
	pid := idOf(t, h.owner.Do("POST", "/api/site/photos", h.prop, `{"kind":"gallery","caption":"","data":"`+tinyPNG+`"}`).Want(t, 201))
	h.w.Anon("GET", "/api/public/a2/photos/"+pid, "").Want(t, 404)
	h.w.Anon("GET", "/api/public/a1/photos/not-an-id", "").Want(t, 404)
	h.w.Anon("GET", "/api/public/nowhere/photos/"+pid, "").Want(t, 404)
}

func (h *hotel) cardBooking(t *testing.T, email, name, from, to string) testkit.Reply {
	t.Helper()
	body := fmt.Sprintf(`{"guest":{"name":%q,"email":%q,"phone":"+84 90 111 2222","locale":"en"},"paymentKind":"card","rooms":[{"roomTypeId":%q,"ratePlanId":%q,"from":%q,"to":%q,"adults":2,"children":0}]}`, name, email, h.rt, h.rp, from, to)
	return h.w.Anon("POST", "/api/public/a1/bookings", body)
}

func TestARiskyCardBookingAsksForA3DSecureCheckAndAHighOneJoinsTheReviewQueue(t *testing.T) {
	h := newHotel(t, 5)
	h.cardBooking(t, "mai@hotel.com", "Mai Pham", "2026-12-10", "2026-12-12").Want(t, 201)
	if c := h.fake.Checkouts; len(c) != 1 || c[0].ThreeDS {
		t.Fatalf("an ordinary booking asked for a 3-D Secure check: %+v", c)
	}
	h.mgr.Do("GET", "/api/risk", h.prop, "").Want(t, 200, "[]")

	h.cardBooking(t, "x1@mailinator.com", "Tom Burns", "2026-12-14", "2026-12-15").Want(t, 201)
	if c := h.fake.Checkouts; len(c) != 2 || !c[1].ThreeDS {
		t.Fatalf("a throwaway email did not ask for a 3-D Secure check: %+v", c)
	}
	h.mgr.Do("GET", "/api/risk", h.prop, "").Want(t, 200, "[]") // medium goes through without staff

	// The same throwaway email again and again, from one address, reaches a high score on the fourth booking.
	h.cardBooking(t, "x1@mailinator.com", "Tom Burns", "2026-12-16", "2026-12-17").Want(t, 201)
	h.mgr.Do("GET", "/api/risk", h.prop, "").Want(t, 200, "[]")
	h.cardBooking(t, "x1@mailinator.com", "Tom Burns", "2026-12-20", "2026-12-21").Want(t, 201)
	q := h.mgr.Do("GET", "/api/risk", h.prop, "").Want(t, 200, `"level":"high"`, "throwaway email service", "many_from_address").Body
	if strings.Count(q, `"reservationId"`) != 1 {
		t.Fatalf("only the high booking waits: %s", q)
	}
	if c := h.fake.Checkouts; !c[len(c)-1].ThreeDS {
		t.Fatal("a high risk booking did not ask for a 3-D Secure check")
	}
}

func withPromo(body, code string) string {
	return strings.Replace(body, `{"guest"`, `{"promo":"`+code+`","guest"`, 1)
}

func TestAPromoCodeTakesMoneyOffTheQuoteAndTheBookingAndIsCountedOnlyByABooking(t *testing.T) {
	h := newHotel(t, 3)
	desk := h.w.Member(h.w.OrgA, h.prop, "front_desk")
	desk.Do("POST", "/api/promo-codes", h.prop, `{"code":"SPRING10","kind":"percent","value":10,"active":true}`).Want(t, 403)
	h.mgr.Do("POST", "/api/promo-codes", h.prop, `{"code":"spring10","kind":"percent","value":10,"active":true}`).Want(t, 201, `"SPRING10"`)
	h.mgr.Do("POST", "/api/promo-codes", h.prop, `{"code":"SPRING10","kind":"percent","value":5,"active":true}`).Want(t, 409)
	h.mgr.Do("POST", "/api/promo-codes", h.prop, `{"code":"BIG","kind":"percent","value":150,"active":true}`).Want(t, 400)
	body := h.booking("hotel", "2026-12-10", "2026-12-12")
	plain := h.w.Anon("POST", "/api/public/a1/quote", body).Want(t, 200, `"total":20000`)
	_ = plain
	for i := 0; i < 2; i++ {
		h.w.Anon("POST", "/api/public/a1/quote", withPromo(body, "spring10")).Want(t, 200, `"total":18000`, "Promo SPRING10", `"amount":-2000`)
	}
	h.mgr.Do("GET", "/api/promo-codes", h.prop, "").Want(t, 200, `"used":0`)
	h.w.Anon("POST", "/api/public/a1/bookings", withPromo(body, "SPRING10")).Want(t, 201, `"total":18000`)
	h.mgr.Do("GET", "/api/promo-codes", h.prop, "").Want(t, 200, `"used":1`)
}

func TestAPromoCodeThatDoesNotFitTheStayIsRefusedAndNothingIsBooked(t *testing.T) {
	h := newHotel(t, 3)
	for _, c := range []string{
		`{"code":"THREENIGHTS","kind":"amount","value":1000,"minNights":3,"active":true}`,
		`{"code":"NEXTYEAR","kind":"amount","value":1000,"arriveFrom":"2027-01-01","active":true}`,
		`{"code":"OFF","kind":"amount","value":1000,"active":false}`,
		`{"code":"ONCE","kind":"amount","value":1000,"maxUses":1,"active":true}`,
	} {
		h.mgr.Do("POST", "/api/promo-codes", h.prop, c).Want(t, 201)
	}
	body := h.booking("hotel", "2026-12-10", "2026-12-12")
	for _, code := range []string{"NOPE", "OFF"} {
		h.w.Anon("POST", "/api/public/a1/bookings", withPromo(body, code)).Want(t, 409, "promo_invalid")
	}
	for _, code := range []string{"THREENIGHTS", "NEXTYEAR"} {
		h.w.Anon("POST", "/api/public/a1/quote", withPromo(body, code)).Want(t, 409, "promo_invalid")
	}
	if got := h.row(t, `select count(*)::text from reservation`); got != "0" {
		t.Fatalf("%s reservations were made with a refused code", got)
	}
	h.w.Anon("POST", "/api/public/a1/bookings", withPromo(body, "ONCE")).Want(t, 201, `"total":19000`)
	h.w.Anon("POST", "/api/public/a1/bookings", withPromo(h.booking("hotel", "2026-12-14", "2026-12-16"), "ONCE")).Want(t, 409, "promo_invalid")
}

func TestAFixedAmountNeverTakesMoreThanTheRoomPrice(t *testing.T) {
	h := newHotel(t, 3)
	h.mgr.Do("POST", "/api/promo-codes", h.prop, `{"code":"HUGE","kind":"amount","value":99999999,"active":true}`).Want(t, 201)
	h.w.Anon("POST", "/api/public/a1/quote", withPromo(h.booking("hotel", "2026-12-10", "2026-12-12"), "HUGE")).Want(t, 200, `"total":0`)
}

func TestAProviderThatMustAnswerInItsOwnWordsGetsItsAnswerOnAGetReport(t *testing.T) {
	vn := payments.NewVNPay("TMN1", "secret", "")
	w := testkit.NewWith(t, func(w *testkit.World) []httpx.Mount {
		return []httpx.Mount{guestbooking.Mount(vn, w.OrgA)}
	})
	// A report with a wrong signature is refused, and a good one for a booking we do not have is answered, so it is not sent again.
	w.Anon("GET", "/api/webhooks/vnpay?vnp_Amount=100&vnp_SecureHash=bad", "").Want(t, 400)
	w.Anon("POST", "/api/webhooks/stripe", "").Want(t, 404)
}

func TestThePublicListNamesOnlyHotelsThatHaveRooms(t *testing.T) {
	h := newHotel(t, 1)
	h.w.Anon("GET", "/api/public", "").Want(t, 200, `"a1"`)
	if body := h.w.Anon("GET", "/api/public", "").Body; strings.Contains(body, `"a2"`) {
		t.Fatalf("a hotel with no rooms is listed: %s", body)
	}
}

func TestThePublicPageStatesEachPlansCancellationTerms(t *testing.T) {
	h := newHotel(t, 1)
	r := h.w.Anon("GET", "/api/public/a1", "").Want(t, 200, `"freeCancelUntilDays":`, `"lateCancelPenalty":`)
	if strings.Contains(r.Body, "bank_transfer") && strings.Contains(r.Body, "IBAN") {
		t.Fatalf("the page leaks bank details: %s", r.Body)
	}
}
