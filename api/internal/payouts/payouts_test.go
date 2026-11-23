package payouts_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/folio"
	"hotelpms/api/internal/payouts"
	"hotelpms/api/internal/rooms"
	"hotelpms/api/internal/testkit"
)

func idOf(t *testing.T, r testkit.Reply) string {
	t.Helper()
	var b struct{ ID string }
	testkit.Must(t, json.Unmarshal([]byte(r.Body), &b))
	return b.ID
}

func TestPaymentsThroughAProviderAreSummedPerDayWithRefundsAndTheNetToExpect(t *testing.T) {
	w := testkit.New(t, rooms.Mount, booking.Mount, folio.Mount, payouts.Mount)
	prop := w.A1
	mgr := w.Member(w.OrgA, &w.A1, "manager")
	desk := w.Member(w.OrgA, &w.A1, "front_desk")
	rt := idOf(t, mgr.Do("POST", "/api/room-types", &prop, `{"name":"Deluxe","baseGuests":2,"maxGuests":3}`).Want(t, 201))
	mgr.Do("POST", "/api/rooms", &prop, fmt.Sprintf(`{"roomTypeId":%q,"number":"101","floor":1}`, rt)).Want(t, 201)
	rp := idOf(t, mgr.Do("POST", "/api/rate-plans", &prop, fmt.Sprintf(`{"roomTypeId":%q,"name":"Flexible"}`, rt)).Want(t, 201))
	mgr.Do("PUT", "/api/rates", &prop, fmt.Sprintf(`{"ratePlanId":%q,"from":"2026-11-23","to":"2027-01-01","price":10000}`, rp)).Want(t, 200)
	res := idOf(t, desk.Do("POST", "/api/reservations", &prop, fmt.Sprintf(`{"guest":{"name":"Mai"},"rooms":[{"roomTypeId":%q,"ratePlanId":%q,"from":"2026-12-10","to":"2026-12-12","adults":2,"children":0}]}`, rt, rp)).Want(t, 201))
	var view struct{ FolioID string }
	testkit.Must(t, json.Unmarshal([]byte(desk.Do("GET", "/api/reservations/"+res+"/folio", &prop, "").Want(t, 200).Body), &view))
	online := idOf(t, desk.Do("POST", "/api/folios/"+view.FolioID+"/payments", &prop, `{"method":"card_terminal","amount":20000}`).Want(t, 201))
	desk.Do("POST", "/api/folios/"+view.FolioID+"/payments", &prop, `{"method":"cash","amount":5000}`).Want(t, 201) // no provider, so not part of a payout
	testkit.Must(t, db.WithOrg(testkit.Ctx, w.Pool, w.OrgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(testkit.Ctx, `update payment set provider = 'stripe' where id = $1`, uuid.MustParse(online))
		return err
	}))
	mgr.Do("POST", "/api/payments/"+online+"/refunds", &prop, `{"amount":3000,"reason":"wrong amount"}`).Want(t, 201, `"status":"done"`)

	desk.Do("GET", "/api/payouts?from=2026-11-01&to=2026-12-31", &prop, "").Want(t, 403)
	got := mgr.Do("GET", "/api/payouts?from=2026-11-01&to=2026-12-31", &prop, "").Want(t, 200, `"provider":"stripe"`, `"gross":20000`, `"refunds":3000`, `"net":17000`, `"payments":1`).Body
	var rows []struct{ Date string }
	testkit.Must(t, json.Unmarshal([]byte(got), &rows))
	if len(rows) != 1 || rows[0].Date != "2026-11-23" {
		t.Fatalf("%s", got)
	}
	mgr.Do("GET", "/api/payouts?from=2026-12-01&to=2026-12-31", &prop, "").Want(t, 200, "[]")
	mgr.Do("GET", "/api/payouts?from=nope&to=2026-12-31", &prop, "").Want(t, 400)
	mgr.Do("GET", "/api/payouts?from=2026-12-31&to=2026-12-01", &prop, "").Want(t, 400)
}
