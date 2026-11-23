package payments_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"hotelpms/api/internal/payments"
)

var stamp = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

func vnpaySign(secret string, q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		if k != "vnp_SecureHash" && k != "vnp_SecureHashType" {
			keys = append(keys, k)
		}
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

func TestVNPayBuildsASignedPaymentAddressInDong(t *testing.T) {
	v := payments.NewVNPay("TMN1", "secret", "https://pay.vnpay.test/vpcpay.html").WithClock(func() time.Time { return stamp })
	c, err := v.CreateCheckout(context.Background(), payments.CheckoutRequest{Reference: "ORG-1:AB12CD34", Amount: 1500000, Currency: "VND", Description: "Booking AB12CD34",
		SuccessURL: "https://x.test/book/a1?done=AB12CD34", CancelURL: "https://x.test/book/a1", Key: "k", ExpiresAt: stamp.Add(30 * time.Minute), ClientIP: "203.0.113.9"})
	if err != nil || !strings.HasPrefix(c.URL, "https://pay.vnpay.test/vpcpay.html?") {
		t.Fatalf("%+v %v", c, err)
	}
	u, _ := url.Parse(c.URL)
	q := u.Query()
	for k, want := range map[string]string{"vnp_TmnCode": "TMN1", "vnp_Amount": "150000000", "vnp_CurrCode": "VND", "vnp_Command": "pay", "vnp_Version": "2.1.0",
		"vnp_IpAddr": "203.0.113.9", "vnp_ReturnUrl": "https://x.test/book/a1?done=AB12CD34", "vnp_CreateDate": "20261123190000", "vnp_ExpireDate": "20261123193000", "vnp_OrderInfo": "ORG-1_AB12CD34"} {
		if q.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, q.Get(k), want)
		}
	}
	if ref := q.Get("vnp_TxnRef"); !strings.HasPrefix(ref, "AB12CD34") || len(ref) > 34 {
		t.Errorf("vnp_TxnRef %q", ref)
	}
	if q.Get("vnp_SecureHash") != vnpaySign("secret", q) {
		t.Fatal("the signature does not match")
	}
	if _, err := v.CreateCheckout(context.Background(), payments.CheckoutRequest{Reference: "O:C", Amount: 100, Currency: "USD"}); !errors.Is(err, payments.ErrCurrency) {
		t.Fatalf("a dollar payment was accepted: %v", err)
	}
}

func vnpayIPN(secret string, edit func(url.Values)) *http.Request {
	q := url.Values{"vnp_TmnCode": {"TMN1"}, "vnp_Amount": {"150000000"}, "vnp_OrderInfo": {"ORG-1_AB12CD34"}, "vnp_TransactionNo": {"14000001"}, "vnp_TxnRef": {"AB12CD34T1"},
		"vnp_ResponseCode": {"00"}, "vnp_TransactionStatus": {"00"}, "vnp_PayDate": {"20261123190500"}}
	if edit != nil {
		edit(q)
	}
	q.Set("vnp_SecureHash", vnpaySign(secret, q))
	return httptest.NewRequest("GET", "/api/webhooks/vnpay?"+q.Encode(), nil)
}

func TestVNPayPaymentReportsAreCheckedBySignatureAndOnlyPaidOnesCount(t *testing.T) {
	v := payments.NewVNPay("TMN1", "secret", "")
	ev, err := v.ParseWebhook(vnpayIPN("secret", nil), stamp)
	if err != nil || ev.Type != payments.Paid || ev.Reference != "ORG-1:AB12CD34" || ev.Amount != 1500000 || ev.Currency != "VND" || ev.ID != "14000001" || ev.PaymentRef != "14000001" {
		t.Fatalf("%+v %v", ev, err)
	}
	if _, err := v.ParseWebhook(vnpayIPN("other", nil), stamp); !errors.Is(err, payments.ErrSignature) {
		t.Fatalf("a forged report was accepted: %v", err)
	}
	r := vnpayIPN("secret", nil)
	q := r.URL.Query()
	q.Set("vnp_Amount", "100")
	r.URL.RawQuery = q.Encode()
	if _, err := v.ParseWebhook(r, stamp); !errors.Is(err, payments.ErrSignature) {
		t.Fatalf("a report with a changed amount was accepted: %v", err)
	}
	if ev, err := v.ParseWebhook(vnpayIPN("secret", func(q url.Values) { q.Set("vnp_ResponseCode", "24") }), stamp); err != nil || ev.Type != payments.Ignored {
		t.Fatalf("a cancelled payment: %+v %v", ev, err)
	}
	w := httptest.NewRecorder()
	v.WriteAck(w)
	if !strings.Contains(w.Body.String(), `"RspCode":"00"`) {
		t.Fatalf("%q", w.Body.String())
	}
	if _, err := v.RefundPayment(context.Background(), "14000001", 100, "k"); !errors.Is(err, payments.ErrManualRefund) {
		t.Fatalf("%v", err)
	}
	if v.Name() != "vnpay" {
		t.Fatal(v.Name())
	}
}

func momoSign(secret string, fields []string, vals map[string]string) string {
	var parts []string
	for _, f := range fields {
		parts = append(parts, f+"="+vals[f])
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(strings.Join(parts, "&")))
	return hex.EncodeToString(m.Sum(nil))
}

var momoCreateFields = []string{"accessKey", "amount", "extraData", "ipnUrl", "orderId", "orderInfo", "partnerCode", "redirectUrl", "requestId", "requestType"}
var momoIPNFields = []string{"accessKey", "amount", "extraData", "message", "orderId", "orderInfo", "orderType", "partnerCode", "payType", "requestId", "responseTime", "resultCode", "transId"}

func TestMoMoAsksForAPaymentAddressWithASignedRequest(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"resultCode":0,"payUrl":"https://pay.momo.test/x","message":"Success"}`))
	}))
	defer srv.Close()
	m := payments.NewMoMo("PART1", "ACCESS1", "secret", srv.URL)
	m.IPNURL = "https://hotel.test/api/webhooks/momo"
	c, err := m.CreateCheckout(context.Background(), payments.CheckoutRequest{Reference: "ORG-1:AB12CD34", Amount: 1500000, Currency: "VND", Description: "Booking AB12CD34", SuccessURL: "https://x.test/done", Key: "k1"})
	if err != nil || c.URL != "https://pay.momo.test/x" {
		t.Fatalf("%+v %v", c, err)
	}
	vals := map[string]string{}
	for k, v := range got {
		vals[k] = fmt.Sprint(v)
	}
	if got["amount"] != float64(1500000) || vals["partnerCode"] != "PART1" || vals["requestType"] != "captureWallet" || vals["ipnUrl"] != "https://hotel.test/api/webhooks/momo" || vals["redirectUrl"] != "https://x.test/done" {
		t.Fatalf("%v", got)
	}
	if ref, _ := base64.StdEncoding.DecodeString(vals["extraData"]); string(ref) != "ORG-1:AB12CD34" {
		t.Fatalf("extraData %q", vals["extraData"])
	}
	vals["accessKey"] = "ACCESS1" // part of what is signed, and never sent
	vals["amount"] = fmt.Sprintf("%.0f", got["amount"])
	if vals["signature"] != momoSign("secret", momoCreateFields, vals) {
		t.Fatal("the signature does not match")
	}
	if _, err := m.CreateCheckout(context.Background(), payments.CheckoutRequest{Reference: "O:C", Amount: 100, Currency: "USD"}); !errors.Is(err, payments.ErrCurrency) {
		t.Fatalf("a dollar payment was accepted: %v", err)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"resultCode":1001,"message":"no"}`))
	}))
	defer bad.Close()
	if _, err := payments.NewMoMo("P", "A", "s", bad.URL).CreateCheckout(context.Background(), payments.CheckoutRequest{Reference: "O:C", Amount: 100, Currency: "VND"}); !errors.Is(err, payments.ErrProvider) {
		t.Fatalf("%v", err)
	}
}

func momoIPN(secret string, edit func(map[string]string)) *http.Request {
	vals := map[string]string{"accessKey": "ACCESS1", "amount": "1500000", "extraData": base64.StdEncoding.EncodeToString([]byte("ORG-1:AB12CD34")), "message": "Success", "orderId": "AB12CD34-1",
		"orderInfo": "Booking AB12CD34", "orderType": "momo_wallet", "partnerCode": "PART1", "payType": "qr", "requestId": "rq1", "responseTime": "1700000000000", "resultCode": "0", "transId": "9000001"}
	if edit != nil {
		edit(vals)
	}
	body := map[string]any{}
	for k, v := range vals {
		body[k] = v
	}
	body["amount"], body["resultCode"], body["transId"] = json.Number(vals["amount"]), json.Number(vals["resultCode"]), json.Number(vals["transId"])
	body["signature"] = momoSign(secret, momoIPNFields, vals)
	b, _ := json.Marshal(body)
	return httptest.NewRequest("POST", "/api/webhooks/momo", strings.NewReader(string(b)))
}

func TestMoMoPaymentReportsAreCheckedBySignatureAndOnlyPaidOnesCount(t *testing.T) {
	m := payments.NewMoMo("PART1", "ACCESS1", "secret", "")
	ev, err := m.ParseWebhook(momoIPN("secret", nil), stamp)
	if err != nil || ev.Type != payments.Paid || ev.Reference != "ORG-1:AB12CD34" || ev.Amount != 1500000 || ev.Currency != "VND" || ev.ID != "9000001" || ev.PaymentRef != "9000001" {
		t.Fatalf("%+v %v", ev, err)
	}
	if _, err := m.ParseWebhook(momoIPN("other", nil), stamp); !errors.Is(err, payments.ErrSignature) {
		t.Fatalf("a forged report was accepted: %v", err)
	}
	if ev, err := m.ParseWebhook(momoIPN("secret", func(v map[string]string) { v["resultCode"] = "1006" }), stamp); err != nil || ev.Type != payments.Ignored {
		t.Fatalf("a declined payment: %+v %v", ev, err)
	}
	if _, err := m.RefundPayment(context.Background(), "9000001", 100, "k"); !errors.Is(err, payments.ErrManualRefund) {
		t.Fatalf("%v", err)
	}
	if m.Name() != "momo" {
		t.Fatal(m.Name())
	}
}
