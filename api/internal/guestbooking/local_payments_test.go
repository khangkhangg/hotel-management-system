package guestbooking_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

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

// newDongHotel is a hotel that charges in Vietnamese dong and takes card payments through the given provider.
func newDongHotel(t *testing.T, card payments.Provider) *hotel {
	t.Helper()
	w := testkit.NewWith(t, func(w *testkit.World) []httpx.Mount {
		return []httpx.Mount{rooms.Mount, extras.Mount, siteprofile.Mount, risk.Mount, promo.Mount, booking.Mount, folio.Mount, guestbooking.Mount(card, w.OrgA)}
	})
	testkit.Must(t, db.WithOrg(testkit.Ctx, w.Pool, w.OrgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(testkit.Ctx, `update property set currency = 'VND' where id = $1`, w.A1)
		return err
	}))
	h := &hotel{w: w, prop: &w.A1}
	h.owner = w.Member(w.OrgA, nil, "owner")
	h.mgr = w.Member(w.OrgA, &w.A1, "manager")
	h.rt = idOf(t, h.mgr.Do("POST", "/api/room-types", h.prop, `{"name":"Deluxe","baseGuests":2,"maxGuests":3}`).Want(t, 201))
	h.mgr.Do("POST", "/api/rooms", h.prop, fmt.Sprintf(`{"roomTypeId":%q,"number":"101","floor":1}`, h.rt)).Want(t, 201)
	h.rp = idOf(t, h.mgr.Do("POST", "/api/rate-plans", h.prop, fmt.Sprintf(`{"roomTypeId":%q,"name":"Flexible"}`, h.rt)).Want(t, 201))
	h.mgr.Do("PUT", "/api/rates", h.prop, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-11-23","to":"2027-01-01","price":1500000}`, h.rp)).Want(t, 200)
	h.owner.Do("PUT", "/api/payment-types/card", h.prop, `{"enabled":true,"holdHours":24,"bankDetails":""}`).Want(t, 204)
	return h
}

func vnpaySigned(secret string, q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, k+"="+url.QueryEscape(q.Get(k)))
	}
	m := hmac.New(sha512.New, []byte(secret))
	m.Write([]byte(strings.Join(parts, "&")))
	return hex.EncodeToString(m.Sum(nil))
}

func TestAVNPayBookingIsPaidOnVNPaysPageAndConfirmedByItsSignedReport(t *testing.T) {
	vn := payments.NewVNPay("TMN1", "secret", "https://pay.vnpay.test/vpcpay.html")
	h := newDongHotel(t, vn)
	m := h.book(t, "card", "2026-12-10", "2026-12-12")
	if !strings.HasPrefix(m.CheckoutURL, "https://pay.vnpay.test/vpcpay.html?") || m.Total != 3000000 || m.Status != "pending" {
		t.Fatalf("%+v", m)
	}
	u, _ := url.Parse(m.CheckoutURL)
	q := u.Query()
	if q.Get("vnp_Amount") != "300000000" || !strings.Contains(q.Get("vnp_OrderInfo"), m.Code) {
		t.Fatalf("%v", q)
	}
	report := func(edit func(url.Values)) string {
		v := url.Values{"vnp_TmnCode": {"TMN1"}, "vnp_Amount": {q.Get("vnp_Amount")}, "vnp_OrderInfo": {q.Get("vnp_OrderInfo")}, "vnp_TransactionNo": {"14000001"},
			"vnp_TxnRef": {q.Get("vnp_TxnRef")}, "vnp_ResponseCode": {"00"}, "vnp_TransactionStatus": {"00"}}
		if edit != nil {
			edit(v)
		}
		v.Set("vnp_SecureHash", vnpaySigned("secret", v))
		return "/api/webhooks/vnpay?" + v.Encode()
	}
	h.w.Anon("GET", report(func(v url.Values) { v.Set("vnp_ResponseCode", "24") }), "").Want(t, 200, `"RspCode":"00"`) // the guest gave up: answered, nothing recorded
	if got := h.row(t, `select status from reservation where code = $1`, m.Code); got != "pending" {
		t.Fatalf("a cancelled payment changed the booking: %s", got)
	}
	h.w.Anon("GET", "/api/webhooks/vnpay?vnp_Amount=1&vnp_SecureHash=bad", "").Want(t, 400)
	h.w.Anon("GET", report(nil), "").Want(t, 200, `"RspCode":"00"`)
	h.w.Anon("GET", report(nil), "").Want(t, 200, `"RspCode":"00"`) // VNPay may send it again
	if got := h.row(t, `select status from reservation where code = $1`, m.Code); got != "confirmed" {
		t.Fatalf("status %s", got)
	}
	if got := h.row(t, `select count(*)::text from payment where provider = 'vnpay' and provider_ref = '14000001' and amount = 3000000`); got != "1" {
		t.Fatalf("%s payments recorded", got)
	}
}

func TestAMoMoBookingIsConfirmedByItsSignedMessageAndAShortPaymentIsHeldForStaff(t *testing.T) {
	momo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"resultCode":0,"payUrl":"https://pay.momo.test/qr/1"}`))
	}))
	defer momo.Close()
	mm := payments.NewMoMo("PART1", "ACCESS1", "secret", momo.URL)
	mm.IPNURL = "https://hotel.test/api/webhooks/momo"
	h := newDongHotel(t, mm)
	h.w.Anon("POST", "/api/webhooks/momo", `not json`).Want(t, 400)
	m := h.book(t, "card", "2026-12-10", "2026-12-12")
	if m.CheckoutURL != "https://pay.momo.test/qr/1" || m.Status != "pending" {
		t.Fatalf("%+v", m)
	}
	org := h.w.OrgA.String()
	report := func(secret string, amount int64) string {
		v := map[string]string{"accessKey": "ACCESS1", "amount": fmt.Sprint(amount), "extraData": base64.StdEncoding.EncodeToString([]byte(org + ":" + m.Code)), "message": "Success", "orderId": m.Code + "-1",
			"orderInfo": "Booking " + m.Code, "orderType": "momo_wallet", "partnerCode": "PART1", "payType": "qr", "requestId": "rq1", "responseTime": "1700000000000", "resultCode": "0", "transId": "9000001"}
		var parts []string
		for _, k := range []string{"accessKey", "amount", "extraData", "message", "orderId", "orderInfo", "orderType", "partnerCode", "payType", "requestId", "responseTime", "resultCode", "transId"} {
			parts = append(parts, k+"="+v[k])
		}
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(strings.Join(parts, "&")))
		body := map[string]any{}
		for k, x := range v {
			body[k] = x
		}
		body["amount"], body["resultCode"], body["transId"] = json.Number(v["amount"]), json.Number("0"), json.Number("9000001")
		body["signature"] = hex.EncodeToString(mac.Sum(nil))
		b, _ := json.Marshal(body)
		return string(b)
	}
	h.w.Anon("POST", "/api/webhooks/momo", report("other", 3000000)).Want(t, 400)
	h.w.Anon("POST", "/api/webhooks/momo", report("secret", 1000000)).Want(t, 204) // not the whole amount
	if got := h.row(t, `select status || ':' || paid_late::text from reservation where code = $1`, m.Code); got != "pending:true" {
		t.Fatalf("a short payment: %s", got)
	}
	// Staff see the held money and decide. Here the rest arrives, as a second payment with its own transaction number.
	if got := h.row(t, `select count(*)::text from payment where provider = 'momo'`); got != "1" {
		t.Fatalf("%s payments", got)
	}
	_ = uuid.Nil
}
