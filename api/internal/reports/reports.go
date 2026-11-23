// Package reports exports a hotel's records as files the owner can open in a spreadsheet.
package reports

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
)

type handler struct{ httpx.Deps }

// Mount adds the exports. People who see reports download them.
func Mount(mux *http.ServeMux, d httpx.Deps) {
	h := &handler{d}
	mux.HandleFunc("/api/reports/reservations.csv", httpx.Methods(map[string]http.HandlerFunc{
		http.MethodGet: httpx.Wrap(access.RequireAny(d, access.ReportsView)(http.HandlerFunc(h.reservations))),
	}))
}

// safe stops a spreadsheet from running a guest's text as a formula: a cell that starts with one of these
// characters gets a leading quote, which spreadsheets show as plain text.
func safe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// zeroDecimal lists the currencies whose smallest unit is the whole unit.
var zeroDecimal = map[string]bool{"VND": true, "JPY": true, "KRW": true, "CLP": true, "IDR": true}

func amount(minor int64, currency string) string {
	sign, n := "", minor
	if n < 0 {
		sign, n = "-", -n
	}
	if zeroDecimal[currency] {
		return fmt.Sprintf("%s%d", sign, n)
	}
	return fmt.Sprintf("%s%d.%02d", sign, n/100, n%100)
}

func period(r *http.Request) (from, to time.Time, ok bool) {
	from, e1 := time.Parse("2006-01-02", r.URL.Query().Get("from"))
	to, e2 := time.Parse("2006-01-02", r.URL.Query().Get("to"))
	return from, to, e1 == nil && e2 == nil && !to.Before(from) && to.Sub(from) <= 366*24*time.Hour
}

// reservations lists the stays that touch the period, one row each, with what was charged and what was paid.
func (h *handler) reservations(w http.ResponseWriter, r *http.Request) {
	a := access.ActorFrom(r.Context())
	from, to, ok := period(r)
	if a.PropertyID == uuid.Nil || !ok {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Choose a property and a period of up to a year, as from and to dates.")
		return
	}
	var out [][]string
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select r.code, r.status, g.name, min(lower(x.stay)), max(upper(x.stay)), count(x.id), r.total, r.currency,
			coalesce((select sum(p.amount) from payment p join folio f on f.id = p.folio_id where f.reservation_id = r.id), 0)
			- coalesce((select sum(rf.amount) from refund rf join payment p on p.id = rf.payment_id join folio f on f.id = p.folio_id where f.reservation_id = r.id and rf.status = 'done'), 0)
			from reservation r join guest g on g.id = r.guest_id join reservation_room x on x.reservation_id = r.id
			where r.property_id = $1 group by r.id, g.name
			having min(lower(x.stay)) <= $3::date and max(upper(x.stay)) > $2::date order by min(lower(x.stay)), r.code`, a.PropertyID, from, to)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var code, status, guest, cur string
			var in, outDay time.Time
			var rooms int
			var total, paid int64
			if err := rows.Scan(&code, &status, &guest, &in, &outDay, &rooms, &total, &cur, &paid); err != nil {
				return err
			}
			out = append(out, []string{safe(code), status, safe(guest), in.Format("2006-01-02"), outDay.Format("2006-01-02"), fmt.Sprint(rooms), amount(total, cur), amount(paid, cur), cur})
		}
		return rows.Err()
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="reservations-`+from.Format("2006-01-02")+`-to-`+to.Format("2006-01-02")+`.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"code", "status", "guest", "arrival", "departure", "rooms", "total", "paid", "currency"})
	_ = cw.WriteAll(out)
}
