package folio_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/folio"
	"hotelpms/api/internal/rooms"
	"hotelpms/api/internal/testkit"
)

func must(t *testing.T, err error) { t.Helper(); testkit.Must(t, err) }

type hotel struct {
	w                *testkit.World
	owner, mgr, desk *testkit.Person
	prop             uuid.UUID
	rt, rp           string
}

func idOf(t *testing.T, r testkit.Reply) string {
	t.Helper()
	var b struct{ ID string }
	must(t, json.Unmarshal([]byte(r.Body), &b))
	return b.ID
}

func newHotel(t *testing.T) *hotel {
	w := testkit.New(t, rooms.Mount, booking.Mount, folio.Mount)
	h := &hotel{w: w, prop: w.A1}
	h.owner = w.Member(w.OrgA, nil, "owner")
	h.mgr = w.Member(w.OrgA, &w.A1, "manager")
	h.desk = w.Member(w.OrgA, &w.A1, "front_desk")
	h.rt = idOf(t, h.mgr.Do("POST", "/api/room-types", &h.prop, `{"name":"Deluxe","baseGuests":2,"maxGuests":3}`).Want(t, 201))
	h.mgr.Do("POST", "/api/rooms", &h.prop, fmt.Sprintf(`{"roomTypeId":%q,"number":"101","floor":1}`, h.rt)).Want(t, 201)
	h.rp = idOf(t, h.mgr.Do("POST", "/api/rate-plans", &h.prop, fmt.Sprintf(`{"roomTypeId":%q,"name":"Flexible"}`, h.rt)).Want(t, 201))
	h.mgr.Do("PUT", "/api/rates", &h.prop, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-11-23","to":"2027-01-01","price":10000}`, h.rp)).Want(t, 200)
	return h
}

// reservation books two nights at 10000 and returns its id.
func (h *hotel) reservation() string {
	h.w.T.Helper()
	return idOf(h.w.T, h.desk.Do("POST", "/api/reservations", &h.prop, fmt.Sprintf(
		`{"guest":{"name":"Mai Pham","email":"mai@hotel.com"},"rooms":[{"roomTypeId":%q,"ratePlanId":%q,"from":"2026-12-10","to":"2026-12-12","adults":2,"children":0}]}`, h.rt, h.rp)).Want(h.w.T, 201))
}

type view struct {
	FolioID  string
	Currency string
	Total    int64
	Paid     int64
	Refunded int64
	Balance  int64
	Lines    []struct {
		ID          string
		Kind        string
		Description string
		Quantity    int
		UnitPrice   int64
		Amount      int64
		TaxIncluded int64
	}
	Payments []struct {
		ID      string
		Method  string
		Amount  int64
		Status  string
		Refunds []struct {
			ID     string
			Amount int64
			Status string
		}
	}
}

func (h *hotel) folio(p *testkit.Person, res string) view {
	h.w.T.Helper()
	var v view
	must(h.w.T, json.Unmarshal([]byte(p.Do("GET", "/api/reservations/"+res+"/folio", &h.prop, "").Want(h.w.T, 200).Body), &v))
	return v
}

func TestTheFolioHasOneLinePerNightAndTheTotalIsTheirSum(t *testing.T) {
	h := newHotel(t)
	res := h.reservation()
	v := h.folio(h.desk, res)
	if len(v.Lines) != 2 || v.Total != 20000 || v.Balance != 20000 || v.Paid != 0 || v.Currency != "USD" {
		t.Fatalf("%+v", v)
	}
	var sum int64
	for _, l := range v.Lines {
		sum += l.Amount
		if l.Kind != "room" {
			t.Fatalf("kind %s", l.Kind)
		}
	}
	if sum != v.Total {
		t.Fatalf("lines add up to %d, total %d", sum, v.Total)
	}
	// Reading it again does not add lines.
	if again := h.folio(h.desk, res); len(again.Lines) != 2 || again.FolioID != v.FolioID {
		t.Fatalf("%+v", again)
	}
}

func TestTaxIsAddedOnTopOrIncludedInThePrice(t *testing.T) {
	h := newHotel(t)
	h.desk.Do("POST", "/api/tax-rules", &h.prop, `{"name":"VAT","percent":10}`).Want(t, 403)
	h.owner.Do("POST", "/api/tax-rules", &h.prop, `{"name":"VAT","percent":10,"inclusive":false}`).Want(t, 201)
	res := h.reservation()
	v := h.folio(h.desk, res)
	if v.Total != 22000 || len(v.Lines) != 3 {
		t.Fatalf("tax on top: %+v", v)
	}
	var tax int64
	for _, l := range v.Lines {
		if l.Kind == "tax" {
			tax = l.Amount
		}
	}
	if tax != 2000 {
		t.Fatalf("tax line %d", tax)
	}

	h2 := newHotel(t)
	h2.owner.Do("POST", "/api/tax-rules", &h2.prop, `{"name":"VAT","percent":10,"inclusive":true}`).Want(t, 201)
	v = h2.folio(h2.desk, h2.reservation())
	if v.Total != 20000 || len(v.Lines) != 2 || v.Lines[0].TaxIncluded != 909 {
		t.Fatalf("tax included: %+v", v)
	}
}

func TestInvalidTaxRulesAreRefused(t *testing.T) {
	h := newHotel(t)
	for _, bad := range []string{`{"name":"","percent":10}`, `{"name":"x","percent":-1}`, `{"name":"x","percent":101}`, `{bad`} {
		h.owner.Do("POST", "/api/tax-rules", &h.prop, bad).Want(t, 400)
	}
}

func TestPaymentsLowerTheBalance(t *testing.T) {
	h := newHotel(t)
	res := h.reservation()
	f := h.folio(h.desk, res).FolioID
	h.desk.Do("POST", "/api/folios/"+f+"/payments", &h.prop, `{"method":"cash","amount":5000,"note":"deposit"}`).Want(t, 201)
	h.desk.Do("POST", "/api/folios/"+f+"/payments", &h.prop, `{"method":"card_terminal","amount":2500,"note":"ref 8841"}`).Want(t, 201)
	v := h.folio(h.desk, res)
	if v.Paid != 7500 || v.Balance != 12500 || len(v.Payments) != 2 {
		t.Fatalf("%+v", v)
	}
	for _, bad := range []string{`{"method":"bitcoin","amount":1}`, `{"method":"cash","amount":0}`, `{"method":"cash","amount":-5}`, `{"method":"cash"}`} {
		h.desk.Do("POST", "/api/folios/"+f+"/payments", &h.prop, bad).Want(t, 400)
	}
	hk := h.w.Member(h.w.OrgA, &h.prop, "housekeeping")
	hk.Do("GET", "/api/reservations/"+res+"/folio", &h.prop, "").Want(t, 403)
	hk.Do("POST", "/api/folios/"+f+"/payments", &h.prop, `{"method":"cash","amount":1}`).Want(t, 403)
	rev := h.w.Member(h.w.OrgA, &h.prop, "revenue")
	rev.Do("POST", "/api/folios/"+f+"/payments", &h.prop, `{"method":"cash","amount":1}`).Want(t, 403)
	h.w.Anon("GET", "/api/reservations/"+res+"/folio", "").Want(t, 401)
}

func TestAnotherPropertyOrOrganizationCannotReachAFolio(t *testing.T) {
	h := newHotel(t)
	res := h.reservation()
	f := h.folio(h.desk, res).FolioID
	other := h.w.Member(h.w.OrgB, &h.w.B1, "front_desk")
	other.Do("GET", "/api/reservations/"+res+"/folio", &h.w.B1, "").Want(t, 404)
	other.Do("POST", "/api/folios/"+f+"/payments", &h.w.B1, `{"method":"cash","amount":1}`).Want(t, 404)
	desk2 := h.w.Member(h.w.OrgA, &h.w.A2, "front_desk")
	desk2.Do("GET", "/api/reservations/"+res+"/folio", &h.w.A2, "").Want(t, 404)
}

func TestManualLinesSurviveAndAdjustTheTotal(t *testing.T) {
	h := newHotel(t)
	res := h.reservation()
	f := h.folio(h.desk, res).FolioID
	h.desk.Do("POST", "/api/folios/"+f+"/lines", &h.prop, `{"kind":"extra","description":"Minibar","quantity":3,"unitPrice":400}`).Want(t, 201)
	h.mgr.Do("POST", "/api/folios/"+f+"/lines", &h.prop, `{"kind":"adjustment","description":"Goodwill discount","quantity":1,"unitPrice":-1500}`).Want(t, 201)
	v := h.folio(h.desk, res)
	if v.Total != 20000+1200-1500 || len(v.Lines) != 4 {
		t.Fatalf("%+v", v)
	}
	h.desk.Do("POST", "/api/folios/"+f+"/lines", &h.prop, `{"kind":"room","description":"x","quantity":1,"unitPrice":1}`).Want(t, 400)
	h.desk.Do("POST", "/api/folios/"+f+"/lines", &h.prop, `{"kind":"extra","description":"","quantity":1,"unitPrice":1}`).Want(t, 400)
	h.desk.Do("POST", "/api/folios/"+f+"/lines", &h.prop, `{"kind":"extra","description":"x","quantity":0,"unitPrice":1}`).Want(t, 400)
}

func pay(t *testing.T, h *hotel, folioID string, amount int) string {
	t.Helper()
	return idOf(t, h.desk.Do("POST", "/api/folios/"+folioID+"/payments", &h.prop, fmt.Sprintf(`{"method":"cash","amount":%d}`, amount)).Want(t, 201))
}

func TestARefundCannotExceedWhatIsLeftOfThePayment(t *testing.T) {
	h := newHotel(t)
	res := h.reservation()
	f := h.folio(h.desk, res).FolioID
	p := pay(t, h, f, 10000)
	h.mgr.Do("POST", "/api/payments/"+p+"/refunds", &h.prop, `{"amount":4000,"reason":"guest left early"}`).Want(t, 201)
	h.mgr.Do("POST", "/api/payments/"+p+"/refunds", &h.prop, `{"amount":6001,"reason":"too much"}`).Want(t, 409, "refund_too_large")
	h.mgr.Do("POST", "/api/payments/"+p+"/refunds", &h.prop, `{"amount":6000,"reason":"the rest"}`).Want(t, 201)
	h.mgr.Do("POST", "/api/payments/"+p+"/refunds", &h.prop, `{"amount":1,"reason":"nothing left"}`).Want(t, 409, "refund_too_large")
	v := h.folio(h.desk, res)
	if v.Paid != 10000 || v.Refunded != 10000 || v.Balance != 20000 || v.Payments[0].Status != "refunded" {
		t.Fatalf("%+v", v)
	}
	for _, bad := range []string{`{"amount":0,"reason":"x"}`, `{"amount":-1,"reason":"x"}`, `{"amount":1,"reason":""}`, `{"amount":1,"reason":"   "}`} {
		h.mgr.Do("POST", "/api/payments/"+p+"/refunds", &h.prop, bad).Want(t, 400)
	}
}

func TestARefundAboveTheRolesLimitWaitsForApproval(t *testing.T) {
	h := newHotel(t)
	res := h.reservation()
	f := h.folio(h.desk, res).FolioID
	p := pay(t, h, f, 20000)
	// Within the front desk limit: done at once.
	h.desk.Do("POST", "/api/payments/"+p+"/refunds", &h.prop, `{"amount":3000,"reason":"wrong amount"}`).Want(t, 201, `"status":"done"`)
	// Above it: waits.
	r := h.desk.Do("POST", "/api/payments/"+p+"/refunds", &h.prop, `{"amount":9000,"reason":"guest unhappy"}`).Want(t, 201, `"status":"pending"`)
	refund := idOf(t, r)
	v := h.folio(h.desk, res)
	if v.Refunded != 3000 || v.Balance != 3000 {
		t.Fatalf("a pending refund changed the balance: %+v", v)
	}
	h.desk.Do("POST", "/api/refunds/"+refund+"/approve", &h.prop, "").Want(t, 403)
	h.mgr.Do("POST", "/api/refunds/"+refund+"/approve", &h.prop, "").Want(t, 204)
	if v := h.folio(h.desk, res); v.Refunded != 12000 || v.Balance != 12000 {
		t.Fatalf("after approval: %+v", v)
	}
	h.mgr.Do("POST", "/api/refunds/"+refund+"/approve", &h.prop, "").Want(t, 409, "already_decided")
	// A pending refund holds its share of the payment, so two cannot add up to more than was paid.
	h.desk.Do("POST", "/api/payments/"+p+"/refunds", &h.prop, `{"amount":8000,"reason":"again"}`).Want(t, 201, `"status":"pending"`)
	h.desk.Do("POST", "/api/payments/"+p+"/refunds", &h.prop, `{"amount":1,"reason":"over"}`).Want(t, 409, "refund_too_large")
}

func TestARejectedRefundFreesItsShare(t *testing.T) {
	h := newHotel(t)
	f := h.folio(h.desk, h.reservation()).FolioID
	p := pay(t, h, f, 20000)
	refund := idOf(t, h.desk.Do("POST", "/api/payments/"+p+"/refunds", &h.prop, `{"amount":9000,"reason":"guest unhappy"}`).Want(t, 201, "pending"))
	h.mgr.Do("POST", "/api/refunds/"+refund+"/reject", &h.prop, "").Want(t, 204)
	h.mgr.Do("POST", "/api/payments/"+p+"/refunds", &h.prop, `{"amount":20000,"reason":"all of it"}`).Want(t, 201)
}

func TestRefundLimitsCanBeChangedByTheOwnerOnly(t *testing.T) {
	h := newHotel(t)
	f := h.folio(h.desk, h.reservation()).FolioID
	p := pay(t, h, f, 20000)
	h.mgr.Do("PUT", "/api/refund-limits", &h.prop, `{"front_desk":15000}`).Want(t, 403)
	h.owner.Do("PUT", "/api/refund-limits", &h.prop, `{"front_desk":15000}`).Want(t, 204)
	h.desk.Do("POST", "/api/payments/"+p+"/refunds", &h.prop, `{"amount":9000,"reason":"guest unhappy"}`).Want(t, 201, `"status":"done"`)
	h.desk.Do("GET", "/api/refund-limits", &h.prop, "").Want(t, 200, "15000")
	h.owner.Do("PUT", "/api/refund-limits", &h.prop, `{"front_desk":-1}`).Want(t, 400)
	h.owner.Do("PUT", "/api/refund-limits", &h.prop, `{"owner":1}`).Want(t, 400)
}

func TestEveryMoneyChangeLeavesATrailEntry(t *testing.T) {
	h := newHotel(t)
	f := h.folio(h.desk, h.reservation()).FolioID
	p := pay(t, h, f, 5000)
	h.mgr.Do("POST", "/api/payments/"+p+"/refunds", &h.prop, `{"amount":100,"reason":"typo"}`).Want(t, 201)
	h.desk.Do("POST", "/api/folios/"+f+"/lines", &h.prop, `{"kind":"extra","description":"Water","quantity":1,"unitPrice":300}`).Want(t, 201)
	h.owner.Do("POST", "/api/tax-rules", &h.prop, `{"name":"VAT","percent":5}`).Want(t, 201)
	var n int
	must(t, dbCount(h, &n))
	if n < 4 {
		t.Fatalf("%d trail entries for 4 money changes", n)
	}
}

func dbCount(h *hotel, n *int) error {
	return db.WithOrg(testkit.Ctx, h.w.Pool, h.w.OrgA, func(tx pgx.Tx) error {
		return tx.QueryRow(testkit.Ctx, `select count(*) from audit_log where action like 'folio.%' or action like 'payment.%' or action like 'refund.%' or action like 'tax_rule.%'`).Scan(n)
	})
}
