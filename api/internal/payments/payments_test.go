package payments_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hotelpms/api/internal/payments"
)

var now = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

func signed(secret string, at time.Time, body string) *http.Request {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.%s", at.Unix(), body)
	r := httptest.NewRequest("POST", "/hook", bytes.NewBufferString(body))
	r.Header.Set("Stripe-Signature", fmt.Sprintf("t=%d,v1=%s", at.Unix(), hex.EncodeToString(mac.Sum(nil))))
	return r
}

const paidBody = `{"id":"evt_1","type":"checkout.session.completed","data":{"object":{"id":"cs_1","client_reference_id":"RES-1","amount_total":22000,"currency":"usd","payment_intent":"pi_1","payment_status":"paid"}}}`

func TestAStripeWebhookWithAGoodSignatureIsAccepted(t *testing.T) {
	s := payments.NewStripe("sk_test", "whsec", "")
	ev, err := s.ParseWebhook(signed("whsec", now, paidBody), now)
	if err != nil {
		t.Fatal(err)
	}
	if ev.ID != "evt_1" || ev.Type != payments.Paid || ev.Reference != "RES-1" || ev.Amount != 22000 || ev.PaymentRef != "pi_1" || ev.Currency != "USD" {
		t.Fatalf("%+v", ev)
	}
}

func TestAForgedOrStaleStripeWebhookIsRefused(t *testing.T) {
	s := payments.NewStripe("sk_test", "whsec", "")
	if _, err := s.ParseWebhook(signed("other-secret", now, paidBody), now); err == nil {
		t.Fatal("forged signature accepted")
	}
	if _, err := s.ParseWebhook(signed("whsec", now.Add(-10*time.Minute), paidBody), now); err == nil {
		t.Fatal("stale timestamp accepted")
	}
	r := signed("whsec", now, paidBody)
	r.Body = http.NoBody
	if _, err := s.ParseWebhook(r, now); err == nil {
		t.Fatal("changed body accepted")
	}
}

func TestStripeCheckoutSendsAnIdempotencyKeyAndNoCardData(t *testing.T) {
	var got *http.Request
	var form string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		b := new(bytes.Buffer)
		b.ReadFrom(r.Body)
		form = b.String()
		w.Write([]byte(`{"id":"cs_9","url":"https://checkout.stripe.test/pay/cs_9"}`))
	}))
	defer srv.Close()
	s := payments.NewStripe("sk_test", "whsec", srv.URL)
	c, err := s.CreateCheckout(context.Background(), payments.CheckoutRequest{
		Reference: "RES-1", Amount: 22000, Currency: "USD", Description: "Deluxe, 2 nights",
		SuccessURL: "https://x.test/ok", CancelURL: "https://x.test/no", Key: "booking-1",
	})
	if err != nil || c.URL != "https://checkout.stripe.test/pay/cs_9" {
		t.Fatalf("%+v %v", c, err)
	}
	if got.Header.Get("Idempotency-Key") != "booking-1" || got.Header.Get("Authorization") != "Bearer sk_test" {
		t.Fatalf("headers %v", got.Header)
	}
	for _, want := range []string{"client_reference_id=RES-1", "mode=payment", "line_items%5B0%5D%5Bprice_data%5D%5Bunit_amount%5D=22000", "price_data%5D%5Bcurrency%5D=usd"} {
		if !strings.Contains(form, want) {
			t.Fatalf("missing %s in %s", want, form)
		}
	}
	if strings.Contains(strings.ToLower(form), "card[") || strings.Contains(strings.ToLower(form), "number") {
		t.Fatalf("card data in request: %s", form)
	}
}

func TestStripeZeroDecimalCurrenciesAreNotMultiplied(t *testing.T) {
	var form string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := new(bytes.Buffer)
		b.ReadFrom(r.Body)
		form = b.String()
		w.Write([]byte(`{"id":"cs_9","url":"u"}`))
	}))
	defer srv.Close()
	s := payments.NewStripe("sk_test", "whsec", srv.URL)
	if _, err := s.CreateCheckout(context.Background(), payments.CheckoutRequest{Reference: "R", Amount: 500000, Currency: "VND", Key: "k"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(form, "unit_amount%5D=500000") {
		t.Fatalf("%s", form)
	}
}

func TestTheFakeProviderRecordsEverythingAndIsRepeatable(t *testing.T) {
	f := payments.NewFake()
	a, _ := f.CreateCheckout(context.Background(), payments.CheckoutRequest{Reference: "R1", Amount: 100, Key: "k1"})
	b, _ := f.CreateCheckout(context.Background(), payments.CheckoutRequest{Reference: "R1", Amount: 100, Key: "k1"})
	if a.ID != b.ID || len(f.Checkouts) != 1 {
		t.Fatalf("same key made %d checkouts", len(f.Checkouts))
	}
	r, err := f.RefundPayment(context.Background(), "pay_1", 50, "rk1")
	if err != nil || r.ID == "" {
		t.Fatalf("%v", err)
	}
	f.FailRefunds = true
	if _, err := f.RefundPayment(context.Background(), "pay_1", 50, "rk2"); err == nil {
		t.Fatal("refund should fail")
	}
}
