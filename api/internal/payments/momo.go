package payments

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultMoMoURL is MoMo's sandbox. A live merchant uses payment.momo.vn.
const DefaultMoMoURL = "https://test-payment.momo.vn"

// MoMo takes wallet and QR payments in Vietnam. The guest pays in the MoMo app and MoMo reports the payment to this server
// with a signed message. It takes only dong, and refunds are made in MoMo's merchant portal.
type MoMo struct {
	partner, access, secret, base string
	IPNURL                        string // the address MoMo reports payments to
	c                             *http.Client
}

// NewMoMo makes the provider. An empty base means the sandbox.
func NewMoMo(partnerCode, accessKey, secretKey, base string) *MoMo {
	if base == "" {
		base = DefaultMoMoURL
	}
	return &MoMo{partner: partnerCode, access: accessKey, secret: secretKey, base: strings.TrimRight(base, "/"), c: &http.Client{Timeout: 20 * time.Second}}
}

func (m *MoMo) Name() string { return "momo" }

func (m *MoMo) sign(fields []string, vals map[string]string) string {
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, f+"="+vals[f])
	}
	h := hmac.New(sha256.New, []byte(m.secret))
	h.Write([]byte(strings.Join(parts, "&")))
	return hex.EncodeToString(h.Sum(nil))
}

var momoCreateFields = []string{"accessKey", "amount", "extraData", "ipnUrl", "orderId", "orderInfo", "partnerCode", "redirectUrl", "requestId", "requestType"}
var momoReportFields = []string{"accessKey", "amount", "extraData", "message", "orderId", "orderInfo", "orderType", "partnerCode", "payType", "requestId", "responseTime", "resultCode", "transId"}

func (m *MoMo) CreateCheckout(ctx context.Context, r CheckoutRequest) (Checkout, error) {
	if !strings.EqualFold(r.Currency, "VND") {
		return Checkout{}, ErrCurrency
	}
	_, code, _ := strings.Cut(r.Reference, ":")
	order := code + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	vals := map[string]string{
		"accessKey": m.access, "amount": strconv.FormatInt(r.Amount, 10), "extraData": base64.StdEncoding.EncodeToString([]byte(r.Reference)), "ipnUrl": m.IPNURL,
		"orderId": order, "orderInfo": r.Description, "partnerCode": m.partner, "redirectUrl": r.SuccessURL, "requestId": order, "requestType": "captureWallet",
	}
	body := map[string]any{"partnerCode": m.partner, "requestId": order, "amount": r.Amount, "orderId": order, "orderInfo": r.Description, "redirectUrl": r.SuccessURL,
		"ipnUrl": m.IPNURL, "lang": "vi", "requestType": "captureWallet", "autoCapture": true, "extraData": vals["extraData"], "signature": m.sign(momoCreateFields, vals)}
	b, err := json.Marshal(body)
	if err != nil {
		return Checkout{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.base+"/v2/gateway/api/create", bytes.NewReader(b))
	if err != nil {
		return Checkout{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.c.Do(req)
	if err != nil {
		return Checkout{}, err
	}
	defer resp.Body.Close()
	var out struct {
		ResultCode int    `json:"resultCode"`
		PayURL     string `json:"payUrl"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil || resp.StatusCode >= 300 || out.ResultCode != 0 || out.PayURL == "" {
		return Checkout{}, fmt.Errorf("%w: MoMo answered %d", ErrProvider, out.ResultCode)
	}
	return Checkout{ID: order, URL: out.PayURL}, nil
}

func (m *MoMo) ParseWebhook(r *http.Request, _ time.Time) (Event, error) {
	d := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	d.UseNumber()
	var raw map[string]any
	if err := d.Decode(&raw); err != nil {
		return Event{}, ErrSignature
	}
	vals := map[string]string{}
	for k, v := range raw {
		vals[k] = fmt.Sprint(v)
	}
	// The access key is ours and is not in MoMo's message, but it is part of what is signed.
	vals["accessKey"] = m.access
	got := vals["signature"]
	if got == "" || !hmac.Equal([]byte(strings.ToLower(got)), []byte(m.sign(momoReportFields, vals))) {
		return Event{}, ErrSignature
	}
	amount, err := strconv.ParseInt(vals["amount"], 10, 64)
	if err != nil {
		return Event{}, ErrSignature
	}
	ref, _ := base64.StdEncoding.DecodeString(vals["extraData"])
	ev := Event{ID: vals["transId"], Type: Ignored, Reference: string(ref), PaymentRef: vals["transId"], Amount: amount, Currency: "VND"}
	if vals["resultCode"] == "0" {
		ev.Type = Paid
	}
	return ev, nil
}

// RefundPayment cannot refund from here: MoMo's refund call needs a separate merchant agreement.
func (m *MoMo) RefundPayment(context.Context, string, int64, string) (Refund, error) {
	return Refund{}, ErrManualRefund
}
