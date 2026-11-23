// Package group is the owner's view across every property of the organization: the numbers of each hotel and their sums.
package group

import (
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
)

// Row is one hotel, or, in the totals, one currency. Money in one currency is never added to money in another.
type Row struct {
	ID       uuid.UUID `json:"id,omitempty"`
	Name     string    `json:"name,omitempty"`
	Currency string    `json:"currency"`
	Rooms    int       `json:"rooms"`
	Occupied int       `json:"occupied"`
	Arriving int       `json:"arriving"`
	Leaving  int       `json:"leaving"`
	Paid30   int64     `json:"paid30"` // payments in the last 30 days, minus refunds made in that time
}

type handler struct{ httpx.Deps }

// Mount adds GET /api/group, for people whose membership covers the whole organization.
func Mount(mux *http.ServeMux, d httpx.Deps) {
	h := &handler{d}
	mux.HandleFunc("/api/group", httpx.Methods(map[string]http.HandlerFunc{
		http.MethodGet: httpx.Wrap(access.RequireAny(d, access.ReportsView)(http.HandlerFunc(h.get))),
	}))
}

func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	a := access.ActorFrom(r.Context())
	// The web app names the property it is on, so the role at that property is not enough:
	// only a membership that covers the whole organization may see every hotel.
	var wide bool
	if err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) (e error) {
		wide, e = access.OrgWide(r.Context(), tx, a)
		return
	}); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	if !wide {
		httpx.Error(w, http.StatusForbidden, "forbidden", "Only people who work across the whole organization can see this.")
		return
	}
	now := h.Clock()
	out := struct {
		Properties []Row `json:"properties"`
		Totals     []Row `json:"totals"`
	}{Properties: []Row{}, Totals: []Row{}}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		ctx := r.Context()
		rows, err := tx.Query(ctx, `select id, name, currency from property order by name`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var p Row
			if err := rows.Scan(&p.ID, &p.Name, &p.Currency); err != nil {
				return err
			}
			out.Properties = append(out.Properties, p)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for i := range out.Properties {
			p := &out.Properties[i]
			today, _, err := booking.HotelToday(ctx, tx, a.OrgID, p.ID, now)
			if err != nil {
				return err
			}
			since := now.Add(-30 * 24 * time.Hour)
			if err := tx.QueryRow(ctx, `select
				(select count(*) from room where property_id = $1),
				(select count(distinct r.id) from reservation r join reservation_room x on x.reservation_id = r.id
				  where r.property_id = $1 and r.status = 'checked_in' and x.stay @> $2::date),
				(select count(distinct r.id) from reservation r join reservation_room x on x.reservation_id = r.id
				  where r.property_id = $1 and r.status in ('confirmed', 'pending') and lower(x.stay) = $2::date),
				(select count(distinct r.id) from reservation r join reservation_room x on x.reservation_id = r.id
				  where r.property_id = $1 and r.status = 'checked_in' and upper(x.stay) = $2::date),
				coalesce((select sum(pay.amount) from payment pay join folio f on f.id = pay.folio_id where f.property_id = $1 and pay.created_at >= $3), 0)
				- coalesce((select sum(rf.amount) from refund rf join payment pay on pay.id = rf.payment_id join folio f on f.id = pay.folio_id
				  where f.property_id = $1 and rf.status = 'done' and rf.decided_at >= $3), 0)`,
				p.ID, today, since).Scan(&p.Rooms, &p.Occupied, &p.Arriving, &p.Leaving, &p.Paid30); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal", "Something went wrong.")
		return
	}
	sums := map[string]*Row{}
	for _, p := range out.Properties {
		s := sums[p.Currency]
		if s == nil {
			s = &Row{Currency: p.Currency}
			sums[p.Currency] = s
		}
		s.Rooms, s.Occupied, s.Arriving, s.Leaving, s.Paid30 = s.Rooms+p.Rooms, s.Occupied+p.Occupied, s.Arriving+p.Arriving, s.Leaving+p.Leaving, s.Paid30+p.Paid30
	}
	for _, s := range sums {
		out.Totals = append(out.Totals, *s)
	}
	sort.Slice(out.Totals, func(i, j int) bool { return out.Totals[i].Currency < out.Totals[j].Currency })
	httpx.JSON(w, http.StatusOK, out)
}
