package guestbooking_test

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"hotelpms/api/internal/testkit"
)

var tokenRe = regexp.MustCompile(`token=([A-Za-z0-9._-]+)`)

// link asks for the manage link and returns its token, read from the email the guest got.
func (h *hotel) link(t *testing.T, code, email string) string {
	t.Helper()
	before := len(h.w.Mail.Sent())
	h.w.Anon("POST", "/api/public/a1/manage-link", fmt.Sprintf(`{"code":%q,"email":%q}`, code, email)).Want(t, 204)
	sent := h.w.Mail.Sent()
	if len(sent) == before {
		return ""
	}
	m := tokenRe.FindStringSubmatch(sent[len(sent)-1].Text)
	if m == nil {
		t.Fatalf("no link in %q", sent[len(sent)-1].Text)
	}
	return m[1]
}

// paidCard books by card and has the webhook confirm it.
func (h *hotel) paidCard(t *testing.T, from, to string) made {
	t.Helper()
	m := h.book(t, "card", from, to)
	h.hook(t, "evt_"+m.Code, "paid", m.Code, m.Total).Want(t, 204)
	return m
}

type view struct {
	Code        string
	Status      string
	Cancellable bool
	Paid        int64
	Refund      int64
	Penalty     int64
}

func (h *hotel) view(t *testing.T, token string, status int) view {
	t.Helper()
	var v view
	r := h.w.Anon("GET", "/api/manage/"+token, "").Want(t, status)
	if status == 200 {
		testkit.Must(t, json.Unmarshal([]byte(r.Body), &v))
	}
	return v
}

func TestTheLinkIsOnlyEmailedToTheGuestWhoseBookingItIs(t *testing.T) {
	h := newHotel(t, 2)
	m := h.paidCard(t, "2026-12-10", "2026-12-12")
	n := len(h.w.Mail.Sent())
	if h.link(t, m.Code, "someone@else.com") != "" || len(h.w.Mail.Sent()) != n {
		t.Fatal("a stranger got a link")
	}
	h.w.Anon("POST", "/api/public/a1/manage-link", `{"code":"NOSUCH","email":"mai@hotel.com"}`).Want(t, 204) // no hint whether a code exists
	if h.link(t, m.Code, "MAI@hotel.com ") == "" {
		t.Fatal("the guest got no link")
	}
}

func TestTheLinkShowsTheBookingAndWhatCancellingWouldGive(t *testing.T) {
	h := newHotel(t, 1)
	m := h.paidCard(t, "2026-12-10", "2026-12-12")
	token := h.link(t, m.Code, "mai@hotel.com")
	v := h.view(t, token, 200)
	if v.Code != m.Code || v.Status != "confirmed" || !v.Cancellable || v.Paid != m.Total || v.Refund != m.Total || v.Penalty != 0 {
		t.Fatalf("%+v", v)
	}
	// Looking is not cancelling.
	if got := h.row(t, `select status from reservation where code = $1`, m.Code); got != "confirmed" {
		t.Fatalf("%s", got)
	}
	h.view(t, "not-a-token", 404)
	h.view(t, h.w.OrgA.String()+".wrongsecret", 404)
	h.w.Advance(2 * time.Hour)
	h.view(t, token, 404)
}

func TestCancellingInsideThePolicyRefundsTheCardThroughTheProvider(t *testing.T) {
	h := newHotel(t, 1)
	m := h.paidCard(t, "2026-12-10", "2026-12-12")
	token := h.link(t, m.Code, "mai@hotel.com")
	h.w.Anon("POST", "/api/manage/"+token+"/cancel", "").Want(t, 200, `"refund":20000`)
	if got := h.row(t, `select status from reservation where code = $1`, m.Code); got != "cancelled" {
		t.Fatalf("%s", got)
	}
	if got := h.row(t, `select coalesce(sum(sold),0)::text from inventory`); got != "0" {
		t.Fatalf("sold %s", got)
	}
	if len(h.fake.Refunds) != 1 || !strings.HasPrefix(h.fake.Refunds[0], "pi_1:20000:") {
		t.Fatalf("%v", h.fake.Refunds)
	}
	if got := h.row(t, `select status from refund limit 1`); got != "done" {
		t.Fatalf("refund %s", got)
	}
	if got := h.row(t, `select status from payment limit 1`); got != "refunded" {
		t.Fatalf("payment %s", got)
	}
	h.w.Anon("POST", "/api/manage/"+token+"/cancel", "").Want(t, 409, "cannot_cancel")
	if len(h.fake.Refunds) != 1 {
		t.Fatal("refunded twice")
	}
}

func TestACancellationInsideTheDeadlineKeepsThePenaltyAndRefundsTheRest(t *testing.T) {
	h := newHotel(t, 1)
	h.row(t, `update rate_plan set free_cancel_until_days = 2, late_cancel_penalty = 'first_night' returning id::text`)
	m := h.paidCard(t, "2026-11-24", "2026-11-26") // tomorrow: past the free deadline
	token := h.link(t, m.Code, "mai@hotel.com")
	v := h.view(t, token, 200)
	if v.Refund != m.Total-10000 || v.Penalty != 0 {
		t.Fatalf("%+v", v)
	}
	h.w.Anon("POST", "/api/manage/"+token+"/cancel", "").Want(t, 200, fmt.Sprintf(`"refund":%d`, m.Total-10000))
}

func TestARefundTheProviderRefusesStaysWaitingForStaff(t *testing.T) {
	h := newHotel(t, 1)
	m := h.paidCard(t, "2026-12-10", "2026-12-12")
	token := h.link(t, m.Code, "mai@hotel.com")
	h.fake.FailRefunds = true
	h.w.Anon("POST", "/api/manage/"+token+"/cancel", "").Want(t, 200, `"refundPending":true`)
	if got := h.row(t, `select status from reservation where code = $1`, m.Code); got != "cancelled" {
		t.Fatalf("%s", got)
	}
	if got := h.row(t, `select status from refund limit 1`); got != "pending" {
		t.Fatalf("refund %s", got)
	}
}

func TestAPayAtTheHotelBookingCancelsWithNothingToRefund(t *testing.T) {
	h := newHotel(t, 1)
	m := h.book(t, "hotel", "2026-12-10", "2026-12-12")
	token := h.link(t, m.Code, "mai@hotel.com")
	h.w.Anon("POST", "/api/manage/"+token+"/cancel", "").Want(t, 200, `"refund":0`)
	if len(h.fake.Refunds) != 0 {
		t.Fatal("refunded money that was never taken")
	}
}

func TestTheGuestCanHaveTheConfirmationSentAgain(t *testing.T) {
	h := newHotel(t, 1)
	m := h.book(t, "hotel", "2026-12-10", "2026-12-12")
	token := h.link(t, m.Code, "mai@hotel.com")
	n := len(h.w.Mail.Sent())
	h.w.Anon("POST", "/api/manage/"+token+"/resend", "").Want(t, 204)
	sent := h.w.Mail.Sent()
	if len(sent) != n+1 || !strings.Contains(sent[n].Text, m.Code) {
		t.Fatalf("%+v", sent)
	}
}
