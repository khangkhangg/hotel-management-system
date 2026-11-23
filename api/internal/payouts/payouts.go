// Package payouts adds up what the payment providers took for the hotel, day by day, less the refunds, so the owner knows
// what to expect in the bank. The real payout schedule belongs to the provider, so this is a guide and not a statement.
package payouts

import (
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
)

// Row is one day of one provider's payments.
type Row struct {
	Date     string `json:"date"`
	Provider string `json:"provider"`
	Currency string `json:"currency"`
	Payments int    `json:"payments"`
	Gross    int64  `json:"gross"`
	Refunds  int64  `json:"refunds"`
	Net      int64  `json:"net"`
}

type handler struct{ httpx.Deps }

// Mount adds the route for owners and managers.
func Mount(mux *http.ServeMux, d httpx.Deps) {
	h := &handler{d}
	mux.HandleFunc("/api/payouts", httpx.Methods(map[string]http.HandlerFunc{http.MethodGet: httpx.Wrap(access.Require(d, access.ReportsView)(http.HandlerFunc(h.get)))}))
}

func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	a := access.ActorFrom(r.Context())
	from, e1 := time.Parse("2006-01-02", r.URL.Query().Get("from"))
	to, e2 := time.Parse("2006-01-02", r.URL.Query().Get("to"))
	if a.PropertyID == uuid.Nil || e1 != nil || e2 != nil || to.Before(from) || to.Sub(from) > 400*24*time.Hour {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Give a property and a range of dates of up to 400 days.")
		return
	}
	type key struct{ date, provider, currency string }
	rows := map[key]*Row{}
	get := func(k key) *Row {
		if rows[k] == nil {
			rows[k] = &Row{Date: k.date, Provider: k.provider, Currency: k.currency}
		}
		return rows[k]
	}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		pays, err := tx.Query(r.Context(), `select to_char((p.created_at at time zone pr.time_zone)::date, 'YYYY-MM-DD'), p.provider, f.currency, count(*), sum(p.amount)
			from payment p join folio f on f.id = p.folio_id join property pr on pr.id = f.property_id
			where f.property_id = $1 and p.provider is not null and (p.created_at at time zone pr.time_zone)::date between $2 and $3 group by 1, 2, 3`, a.PropertyID, from, to)
		if err != nil {
			return err
		}
		for pays.Next() {
			var k key
			var n int
			var sum int64
			if err := pays.Scan(&k.date, &k.provider, &k.currency, &n, &sum); err != nil {
				pays.Close()
				return err
			}
			x := get(k)
			x.Payments, x.Gross = n, sum
		}
		pays.Close()
		if err := pays.Err(); err != nil {
			return err
		}
		refs, err := tx.Query(r.Context(), `select to_char((x.decided_at at time zone pr.time_zone)::date, 'YYYY-MM-DD'), p.provider, f.currency, sum(x.amount)
			from refund x join payment p on p.id = x.payment_id join folio f on f.id = p.folio_id join property pr on pr.id = f.property_id
			where f.property_id = $1 and p.provider is not null and x.status = 'done' and (x.decided_at at time zone pr.time_zone)::date between $2 and $3 group by 1, 2, 3`, a.PropertyID, from, to)
		if err != nil {
			return err
		}
		defer refs.Close()
		for refs.Next() {
			var k key
			var sum int64
			if err := refs.Scan(&k.date, &k.provider, &k.currency, &sum); err != nil {
				return err
			}
			get(k).Refunds = sum
		}
		return refs.Err()
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	out := make([]Row, 0, len(rows))
	for _, x := range rows {
		x.Net = x.Gross - x.Refunds
		out = append(out, *x)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].Provider < out[j].Provider
	})
	httpx.JSON(w, http.StatusOK, out)
}
