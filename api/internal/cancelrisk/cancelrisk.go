// Package cancelrisk scores how likely a reservation is to be cancelled, with a few plain rules and the reasons in
// words, so staff can trust it. It changes nothing by itself: it only informs the people who plan the stock.
package cancelrisk

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
)

// Signals are what is known about a reservation.
type Signals struct {
	LeadDays     int    // days between the booking and the arrival
	Source       string // direct, website, or the booking site's name
	Paid         bool   // some money was paid online or at the desk
	Nights       int
	PriorCancels int // other reservations of the same guest that were cancelled or not used
}

// Reason is one thing that raised the score.
type Reason struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// Result is the score, its level and the reasons.
type Result struct {
	Score   int      `json:"score"`
	Level   string   `json:"level"` // low, medium or high
	Reasons []Reason `json:"reasons"`
}

// Score adds the weight of each signal. The weights are rough rules of thumb, not a trained model, and the screen says so.
func Score(s Signals) Result {
	r := Result{Reasons: []Reason{}}
	add := func(n int, code, text string) {
		r.Score += n
		r.Reasons = append(r.Reasons, Reason{code, text})
	}
	switch {
	case s.LeadDays >= 60:
		add(25, "very_early", "The booking was made 60 or more days ahead of the stay.")
	case s.LeadDays >= 30:
		add(15, "early", "The booking was made 30 or more days ahead of the stay.")
	}
	if s.Source != "" && s.Source != "direct" && s.Source != "website" {
		add(15, "booking_site", "The booking came from a booking site, where guests cancel more often.")
	}
	if !s.Paid {
		add(20, "nothing_paid", "Nothing has been paid yet.")
	}
	switch {
	case s.PriorCancels >= 2:
		add(35, "past_cancels", "This guest cancelled or did not come for two or more earlier bookings.")
	case s.PriorCancels == 1:
		add(20, "past_cancel", "This guest cancelled or did not come for an earlier booking.")
	}
	switch {
	case r.Score >= 60:
		r.Level = "high"
	case r.Score >= 30:
		r.Level = "medium"
	default:
		r.Level = "low"
	}
	return r
}

// Item is one reservation with its score.
type Item struct {
	ReservationID uuid.UUID `json:"reservationId"`
	Code          string    `json:"code"`
	Guest         string    `json:"guest"`
	Arrival       string    `json:"arrival"`
	Result
}

const selectItems = `select r.id, r.code, g.name, to_char(a.arrival, 'YYYY-MM-DD'), a.arrival, a.nights, r.source_channel, r.created_at,
	coalesce((select sum(p.amount) from payment p join folio f on f.id = p.folio_id where f.reservation_id = r.id and p.status in ('paid', 'partly_refunded')), 0) > 0,
	(select count(*) from reservation r2 join guest g2 on g2.id = r2.guest_id where g.email <> '' and lower(g2.email) = lower(g.email) and r2.id <> r.id and r2.status in ('cancelled', 'no_show'))
	from reservation r join guest g on g.id = r.guest_id
	join lateral (select min(lower(x.stay)) arrival, max(upper(x.stay)) - min(lower(x.stay)) nights from reservation_room x where x.reservation_id = r.id) a on a.arrival is not null
	where r.property_id = $1`

func scan(rows pgx.Rows) ([]Item, error) {
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		var it Item
		var arrival, created time.Time
		var nights, prior int
		var source string
		var paid bool
		if err := rows.Scan(&it.ReservationID, &it.Code, &it.Guest, &it.Arrival, &arrival, &nights, &source, &created, &paid, &prior); err != nil {
			return nil, err
		}
		lead := int(arrival.Sub(time.Date(created.Year(), created.Month(), created.Day(), 0, 0, 0, 0, time.UTC)).Hours() / 24)
		it.Result = Score(Signals{LeadDays: lead, Source: source, Paid: paid, Nights: nights, PriorCancels: prior})
		out = append(out, it)
	}
	return out, rows.Err()
}

// ForReservation scores one reservation, or returns pgx.ErrNoRows.
func ForReservation(ctx context.Context, tx pgx.Tx, prop, id uuid.UUID) (Item, error) {
	rows, err := tx.Query(ctx, selectItems+` and r.id = $2`, prop, id)
	if err != nil {
		return Item{}, err
	}
	items, err := scan(rows)
	if err != nil {
		return Item{}, err
	}
	if len(items) == 0 {
		return Item{}, pgx.ErrNoRows
	}
	return items[0], nil
}

// Upcoming scores the reservations that arrive from the hotel's today until the given day, riskiest first.
func Upcoming(ctx context.Context, tx pgx.Tx, prop uuid.UUID, from, to time.Time) ([]Item, error) {
	rows, err := tx.Query(ctx, selectItems+` and r.status in ('pending', 'confirmed') and a.arrival >= $2::date and a.arrival <= $3::date`, prop, from, to)
	if err != nil {
		return nil, err
	}
	items, err := scan(rows)
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(items, func(a, b Item) int { return b.Score - a.Score })
	return items, nil
}

type handler struct{ httpx.Deps }

// Mount adds the routes for people who see reservations.
func Mount(mux *http.ServeMux, d httpx.Deps) {
	h := &handler{d}
	wrap := func(f http.HandlerFunc) http.HandlerFunc {
		return httpx.Methods(map[string]http.HandlerFunc{http.MethodGet: httpx.Wrap(access.Require(d, access.ReservationsView)(f))})
	}
	mux.HandleFunc("/api/reservations/{id}/cancel-risk", wrap(h.one))
	mux.HandleFunc("/api/cancel-risk", wrap(h.upcoming))
}

func (h *handler) one(w http.ResponseWriter, r *http.Request) {
	a := access.ActorFrom(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil || a.PropertyID == uuid.Nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
		return
	}
	var out Item
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var e error
		out, e = ForReservation(r.Context(), tx, a.PropertyID, id)
		return e
	})
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *handler) upcoming(w http.ResponseWriter, r *http.Request) {
	a := access.ActorFrom(r.Context())
	if a.PropertyID == uuid.Nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Choose a property first.")
		return
	}
	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil || days < 1 || days > 365 {
		days = 30
	}
	var out []Item
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		today, _, e := booking.HotelToday(r.Context(), tx, a.OrgID, a.PropertyID, h.Clock())
		if e != nil {
			return e
		}
		out, e = Upcoming(r.Context(), tx, a.PropertyID, today, today.AddDate(0, 0, days))
		return e
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}
