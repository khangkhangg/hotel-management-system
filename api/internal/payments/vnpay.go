package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultVNPayURL is VNPay's sandbox payment page. A live merchant is given another address.
const DefaultVNPayURL = "https://sandbox.vnpayment.vn/paymentv2/vpcpay.html"

// VNPay takes bank transfers, QR codes and cards in Vietnam. The guest pays on VNPay's page and VNPay reports the payment
// with a signed address on this server. It takes only dong, and refunds are made in VNPay's merchant portal.
type VNPay struct {
	tmn, secret, payURL string
	now                 func() time.Time
}

// NewVNPay makes the provider. An empty payURL means the sandbox.
func NewVNPay(tmnCode, hashSecret, payURL string) *VNPay {
	if payURL == "" {
		payURL = DefaultVNPayURL
	}
	return &VNPay{tmnCode, hashSecret, payURL, time.Now}
}

// WithClock sets the clock, for tests.
func (v *VNPay) WithClock(now func() time.Time) *VNPay { v.now = now; return v }

func (v *VNPay) Name() string { return "vnpay" }

var vietnam = time.FixedZone("ICT", 7*3600)

func (v *VNPay) sign(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		if k != "vnp_SecureHash" && k != "vnp_SecureHashType" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+url.QueryEscape(q.Get(k)))
	}
	m := hmac.New(sha512.New, []byte(v.secret))
	m.Write([]byte(strings.Join(parts, "&")))
	return hex.EncodeToString(m.Sum(nil))
}

func (v *VNPay) CreateCheckout(_ context.Context, r CheckoutRequest) (Checkout, error) {
	if !strings.EqualFold(r.Currency, "VND") {
		return Checkout{}, ErrCurrency
	}
	now := v.now()
	_, code, _ := strings.Cut(r.Reference, ":")
	ref := code + "T" + strconv.FormatInt(now.Unix(), 36)
	ip := r.ClientIP
	if ip == "" {
		ip = "127.0.0.1"
	}
	q := url.Values{
		"vnp_Version": {"2.1.0"}, "vnp_Command": {"pay"}, "vnp_TmnCode": {v.tmn}, "vnp_Amount": {strconv.FormatInt(r.Amount*100, 10)},
		"vnp_CreateDate": {now.In(vietnam).Format("20060102150405")}, "vnp_CurrCode": {"VND"}, "vnp_IpAddr": {ip}, "vnp_Locale": {"vn"},
		// The organization and booking code travel in the order text, with no colon, and come back in the payment report.
		"vnp_OrderInfo": {strings.Replace(r.Reference, ":", "_", 1)}, "vnp_OrderType": {"other"}, "vnp_ReturnUrl": {r.SuccessURL}, "vnp_TxnRef": {ref},
	}
	if !r.ExpiresAt.IsZero() {
		q.Set("vnp_ExpireDate", r.ExpiresAt.In(vietnam).Format("20060102150405"))
	}
	q.Set("vnp_SecureHash", v.sign(q))
	return Checkout{ID: ref, URL: v.payURL + "?" + q.Encode()}, nil
}

func (v *VNPay) ParseWebhook(r *http.Request, _ time.Time) (Event, error) {
	q := r.URL.Query()
	got, want := q.Get("vnp_SecureHash"), v.sign(q)
	if got == "" || !hmac.Equal([]byte(strings.ToLower(got)), []byte(want)) {
		return Event{}, ErrSignature
	}
	amount, err := strconv.ParseInt(q.Get("vnp_Amount"), 10, 64)
	if err != nil {
		return Event{}, ErrSignature
	}
	paid := q.Get("vnp_ResponseCode") == "00" && q.Get("vnp_TransactionStatus") == "00"
	ev := Event{ID: q.Get("vnp_TransactionNo"), Type: Ignored, Reference: strings.Replace(q.Get("vnp_OrderInfo"), "_", ":", 1),
		PaymentRef: q.Get("vnp_TransactionNo"), Amount: amount / 100, Currency: "VND"}
	if paid {
		ev.Type = Paid
	}
	return ev, nil
}

// WriteAck gives the answer VNPay waits for. Without it VNPay sends the report again.
func (v *VNPay) WriteAck(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"RspCode":"00","Message":"Confirm Success"}`))
}

// RefundPayment cannot refund from here: VNPay's refund call needs a separate merchant agreement.
func (v *VNPay) RefundPayment(context.Context, string, int64, string) (Refund, error) {
	return Refund{}, ErrManualRefund
}
