// Package payments talks to card providers (spec 06). Card data is entered on the provider's own page, so nothing here ever holds a card number.
package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// What happened, as the hotel system understands it.
const (
	Paid     = "paid"
	Failed   = "failed"
	Refunded = "refunded"
	Ignored  = "ignored" // an event type the system does not act on
)

var (
	ErrSignature = errors.New("payments: the webhook signature is wrong or too old")
	ErrProvider  = errors.New("payments: the provider refused the request")
)

type CheckoutRequest struct {
	Reference   string // our booking code, echoed back by the webhook
	Amount      int64  // minor units
	Currency    string
	Description string
	SuccessURL  string
	CancelURL   string
	Key         string    // idempotency key: the same key never charges twice
	ExpiresAt   time.Time // after this the payment page no longer takes money, so a late payment cannot happen
	ThreeDS     bool      // ask the card for a 3-D Secure check, which moves a fraud chargeback to the card issuer
}

type Checkout struct{ ID, URL string }

type Event struct {
	ID         string // unique per provider event, so a repeat can be recognised
	Type       string
	Reference  string
	PaymentRef string // the provider's id for the payment, needed to refund it
	Amount     int64
	Currency   string
}

type Refund struct{ ID string }

type Provider interface {
	Name() string // the name in the webhook address
	CreateCheckout(ctx context.Context, r CheckoutRequest) (Checkout, error)
	ParseWebhook(r *http.Request, now time.Time) (Event, error)
	RefundPayment(ctx context.Context, paymentRef string, amount int64, key string) (Refund, error)
}

// Stripe uses hosted Checkout: the guest pays on Stripe's page and a signed webhook tells us.
type Stripe struct {
	secretKey, webhookSecret, base string
	client                         *http.Client
}

// NewStripe builds the provider. An empty base means Stripe's real address.
func NewStripe(secretKey, webhookSecret, base string) *Stripe {
	if base == "" {
		base = "https://api.stripe.com"
	}
	return &Stripe{secretKey, webhookSecret, base, &http.Client{Timeout: 15 * time.Second}}
}

// stripeAmount converts our minor units to Stripe's. Stripe counts rupiah in hundredths although we hold whole rupiah.
func stripeAmount(amount int64, currency string) int64 {
	if strings.EqualFold(currency, "IDR") {
		return amount * 100
	}
	return amount
}

func (s *Stripe) post(ctx context.Context, path, key string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, "POST", s.base+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.secretKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Idempotency-Key", key)
	res, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return fmt.Errorf("%w: %d", ErrProvider, res.StatusCode)
	}
	return json.Unmarshal(body, out)
}

func (s *Stripe) Name() string { return "stripe" }

func (s *Stripe) CreateCheckout(ctx context.Context, r CheckoutRequest) (Checkout, error) {
	f := url.Values{}
	f.Set("mode", "payment")
	f.Set("client_reference_id", r.Reference)
	f.Set("success_url", r.SuccessURL)
	f.Set("cancel_url", r.CancelURL)
	if !r.ExpiresAt.IsZero() {
		f.Set("expires_at", strconv.FormatInt(r.ExpiresAt.Unix(), 10))
	}
	if r.ThreeDS {
		f.Set("payment_method_options[card][request_three_d_secure]", "any")
	}
	f.Set("line_items[0][quantity]", "1")
	f.Set("line_items[0][price_data][currency]", strings.ToLower(r.Currency))
	f.Set("line_items[0][price_data][unit_amount]", strconv.FormatInt(stripeAmount(r.Amount, r.Currency), 10))
	f.Set("line_items[0][price_data][product_data][name]", r.Description)
	var out struct{ ID, URL string }
	if err := s.post(ctx, "/v1/checkout/sessions", r.Key, f, &out); err != nil {
		return Checkout{}, err
	}
	return Checkout{ID: out.ID, URL: out.URL}, nil
}

func (s *Stripe) RefundPayment(ctx context.Context, paymentRef string, amount int64, key string) (Refund, error) {
	f := url.Values{"payment_intent": {paymentRef}, "amount": {strconv.FormatInt(amount, 10)}}
	var out struct{ ID string }
	if err := s.post(ctx, "/v1/refunds", key, f, &out); err != nil {
		return Refund{}, err
	}
	return Refund{ID: out.ID}, nil
}

// ParseWebhook checks the Stripe-Signature header (t=<unix>,v1=<hmac of "t.body">) and reads the event.
func (s *Stripe) ParseWebhook(r *http.Request, now time.Time) (Event, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return Event{}, err
	}
	var ts string
	var sigs []string
	for _, part := range strings.Split(r.Header.Get("Stripe-Signature"), ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || len(sigs) == 0 || now.Sub(time.Unix(sec, 0)).Abs() > 5*time.Minute {
		return Event{}, ErrSignature
	}
	mac := hmac.New(sha256.New, []byte(s.webhookSecret))
	fmt.Fprintf(mac, "%s.%s", ts, body)
	want := hex.EncodeToString(mac.Sum(nil))
	ok := false
	for _, sig := range sigs {
		if hmac.Equal([]byte(sig), []byte(want)) {
			ok = true
		}
	}
	if !ok {
		return Event{}, ErrSignature
	}
	var in struct {
		ID, Type string
		Data     struct {
			Object struct {
				ID                string `json:"id"`
				ClientReferenceID string `json:"client_reference_id"`
				AmountTotal       int64  `json:"amount_total"`
				Currency          string `json:"currency"`
				PaymentIntent     string `json:"payment_intent"`
				PaymentStatus     string `json:"payment_status"`
			}
		}
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return Event{}, err
	}
	o := in.Data.Object
	ev := Event{ID: in.ID, Reference: o.ClientReferenceID, PaymentRef: o.PaymentIntent, Currency: strings.ToUpper(o.Currency), Type: Ignored}
	ev.Amount = o.AmountTotal
	if strings.EqualFold(o.Currency, "IDR") {
		ev.Amount /= 100
	}
	switch {
	case in.Type == "checkout.session.completed" && o.PaymentStatus == "paid", in.Type == "checkout.session.async_payment_succeeded":
		ev.Type = Paid
	case in.Type == "checkout.session.async_payment_failed", in.Type == "checkout.session.expired":
		ev.Type = Failed
	}
	return ev, nil
}

// Fake is the provider tests use. It records what it was asked and never calls out.
type Fake struct {
	mu           sync.Mutex
	Checkouts    []CheckoutRequest
	Refunds      []string
	FailRefunds  bool
	FailCheckout bool
	byKey        map[string]Checkout
}

func NewFake() *Fake { return &Fake{byKey: map[string]Checkout{}} }

func (f *Fake) Name() string { return "fake" }

func (f *Fake) CreateCheckout(_ context.Context, r CheckoutRequest) (Checkout, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FailCheckout {
		return Checkout{}, ErrProvider
	}
	if c, ok := f.byKey[r.Key]; ok {
		return c, nil
	}
	f.Checkouts = append(f.Checkouts, r)
	c := Checkout{ID: "cs_fake_" + r.Key, URL: "https://pay.fake.test/" + r.Key}
	f.byKey[r.Key] = c
	return c, nil
}

func (f *Fake) RefundPayment(_ context.Context, paymentRef string, amount int64, key string) (Refund, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FailRefunds {
		return Refund{}, ErrProvider
	}
	f.Refunds = append(f.Refunds, fmt.Sprintf("%s:%d:%s", paymentRef, amount, key))
	return Refund{ID: "re_fake_" + key}, nil
}

// ParseWebhook reads the fake's own JSON event: {"id","type","reference","paymentRef","amount","currency"}.
func (f *Fake) ParseWebhook(r *http.Request, _ time.Time) (Event, error) {
	var ev Event
	if r.Header.Get("X-Fake-Signature") != "ok" {
		return Event{}, ErrSignature
	}
	var in struct {
		ID, Type, Reference, PaymentRef, Currency string
		Amount                                    int64
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		return Event{}, err
	}
	ev = Event{ID: in.ID, Type: in.Type, Reference: in.Reference, PaymentRef: in.PaymentRef, Amount: in.Amount, Currency: in.Currency}
	return ev, nil
}
