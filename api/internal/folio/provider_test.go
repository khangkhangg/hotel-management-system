package folio_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/db"
	"hotelpms/api/internal/payments"
	"hotelpms/api/internal/testkit"
)

// cardPayment books a stay, takes 20000 by hand and marks it as paid through the card provider.
func cardPayment(t *testing.T, h *hotel) (p, res string) {
	t.Helper()
	res = h.reservation()
	f := h.folio(h.desk, res).FolioID
	p = pay(t, h, f, 20000)
	testkit.Must(t, db.WithOrg(testkit.Ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(testkit.Ctx, `update payment set provider = 'fake', provider_ref = 'pi_9' where id = $1`, uuid.MustParse(p))
		return err
	}))
	return p, res
}

func TestARefundOfACardPaymentGoesToTheCardProviderAndARefusalStopsIt(t *testing.T) {
	card := payments.NewFake()
	h := newHotelCard(t, card)
	p, res := cardPayment(t, h)
	card.FailRefunds = true
	h.desk.Do("POST", "/api/payments/"+p+"/refunds", &h.prop, `{"amount":3000,"reason":"wrong amount"}`).Want(t, 502, "provider_refused")
	if v := h.folio(h.desk, res); v.Refunded != 0 {
		t.Fatalf("a refused refund was recorded: %+v", v)
	}
	card.FailRefunds = false
	h.desk.Do("POST", "/api/payments/"+p+"/refunds", &h.prop, `{"amount":3000,"reason":"wrong amount"}`).Want(t, 201, `"status":"done"`)
	if len(card.Refunds) != 1 || card.Refunds[0][:8] != "pi_9:300" {
		t.Fatalf("%v", card.Refunds)
	}
}

func TestAWaitingCardRefundIsSentWhenApprovedAndStaysWaitingIfRefused(t *testing.T) {
	card := payments.NewFake()
	h := newHotelCard(t, card)
	p, _ := cardPayment(t, h)
	r := idOf(t, h.desk.Do("POST", "/api/payments/"+p+"/refunds", &h.prop, `{"amount":9000,"reason":"guest unhappy"}`).Want(t, 201, `"status":"pending"`))
	if len(card.Refunds) != 0 {
		t.Fatalf("sent before approval: %v", card.Refunds)
	}
	card.FailRefunds = true
	h.mgr.Do("POST", "/api/refunds/"+r+"/approve", &h.prop, "").Want(t, 502, "provider_refused")
	card.FailRefunds = false
	h.mgr.Do("POST", "/api/refunds/"+r+"/approve", &h.prop, "").Want(t, 204)
	if len(card.Refunds) != 1 || card.Refunds[0] != fmt.Sprintf("pi_9:9000:refund-%s", r) {
		t.Fatalf("%v", card.Refunds)
	}
}

func TestACardPaymentCannotBeRefundedWhenNoCardProviderIsSetUp(t *testing.T) {
	h := newHotel(t)
	p, _ := cardPayment(t, h)
	h.desk.Do("POST", "/api/payments/"+p+"/refunds", &h.prop, `{"amount":3000,"reason":"wrong amount"}`).Want(t, 502, "provider_refused")
}

func TestAProviderWithNoRefundCallStillLetsStaffRecordTheRefund(t *testing.T) {
	card := payments.NewFake()
	card.Manual = true
	h := newHotelCard(t, card)
	p, res := cardPayment(t, h)
	h.desk.Do("POST", "/api/payments/"+p+"/refunds", &h.prop, `{"amount":3000,"reason":"wrong amount"}`).Want(t, 201, `"status":"done"`)
	if v := h.folio(h.desk, res); v.Refunded != 3000 {
		t.Fatalf("%+v", v)
	}
}
