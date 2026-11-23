package pricing

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/ids"
)

type handler struct{ httpx.Deps }

// Mount adds the routes. People who set rates use them.
func Mount(mux *http.ServeMux, d httpx.Deps) {
	h := &handler{d}
	one := func(methods map[string]http.HandlerFunc) http.HandlerFunc {
		m := map[string]http.HandlerFunc{}
		for k, f := range methods {
			m[k] = httpx.Wrap(access.Require(d, access.RatesEdit)(f))
		}
		return httpx.Methods(m)
	}
	mux.HandleFunc("/api/price-suggestions", one(map[string]http.HandlerFunc{http.MethodGet: h.suggest}))
	mux.HandleFunc("/api/price-suggestions/apply", one(map[string]http.HandlerFunc{http.MethodPost: h.apply}))
	mux.HandleFunc("/api/price-limits", one(map[string]http.HandlerFunc{http.MethodGet: h.getLimits, http.MethodPut: h.putLimit}))
	mux.HandleFunc("/api/competitor-rates", one(map[string]http.HandlerFunc{http.MethodGet: h.listCompetitors, http.MethodPost: h.postCompetitors}))
	mux.HandleFunc("/api/competitor-rates/{id}", one(map[string]http.HandlerFunc{http.MethodDelete: h.deleteCompetitor}))
}

func scope(w http.ResponseWriter, r *http.Request) (access.Actor, bool) {
	a := access.ActorFrom(r.Context())
	if a.PropertyID == uuid.Nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Choose a property first.")
		return a, false
	}
	return a, true
}

var errBad = errors.New("pricing: bad input")
var errOutside = errors.New("pricing: the price is outside the limits")

func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
	case errors.Is(err, errBad):
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Check the details and try again.")
	case errors.Is(err, errOutside):
		httpx.Error(w, http.StatusBadRequest, "outside_limits", "That price is outside the floor and ceiling you set.")
	default:
		httpx.InternalError(w, err)
	}
}

func read(w http.ResponseWriter, r *http.Request, dst any) bool {
	d := json.NewDecoder(io.LimitReader(r.Body, 128<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Check the details and try again.")
		return false
	}
	return true
}

type limit struct{ floor, ceiling *int64 }

func loadLimits(tx pgx.Tx, r *http.Request, prop uuid.UUID) (map[uuid.UUID]limit, error) {
	rows, err := tx.Query(r.Context(), `select room_type_id, floor_price, ceiling_price from price_limit where property_id = $1`, prop)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]limit{}
	for rows.Next() {
		var id uuid.UUID
		var l limit
		if err := rows.Scan(&id, &l.floor, &l.ceiling); err != nil {
			return nil, err
		}
		out[id] = l
	}
	return out, rows.Err()
}

type suggestionOut struct {
	Date         string    `json:"date"`
	RatePlanID   uuid.UUID `json:"ratePlanId"`
	RatePlanName string    `json:"ratePlanName"`
	RoomTypeID   uuid.UUID `json:"roomTypeId"`
	RoomType     string    `json:"roomType"`
	Current      int64     `json:"current"`
	Price        int64     `json:"price"`
	Occupancy    float64   `json:"occupancy"`
	Competitor   *int64    `json:"competitor"`
	Reasons      []string  `json:"reasons"`
}

func (h *handler) suggest(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil || days < 1 || days > 120 {
		days = 30
	}
	out := []suggestionOut{}
	now := h.Clock()
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		today, currency, err := booking.HotelToday(r.Context(), tx, a.OrgID, a.PropertyID, now)
		if err != nil {
			return err
		}
		limits, err := loadLimits(tx, r, a.PropertyID)
		if err != nil {
			return err
		}
		prices := map[string][]int64{}
		crows, err := tx.Query(r.Context(), `select to_char(date, 'YYYY-MM-DD'), price from competitor_rate where property_id = $1 and date >= $2::date and date < $2::date + $3::int`, a.PropertyID, today, days)
		if err != nil {
			return err
		}
		for crows.Next() {
			var d string
			var p int64
			if err := crows.Scan(&d, &p); err != nil {
				crows.Close()
				return err
			}
			prices[d] = append(prices[d], p)
		}
		crows.Close()
		if err := crows.Err(); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), `select rp.id, rp.name, rp.room_type_id, t.name, to_char(r.date, 'YYYY-MM-DD'), r.date, r.price, i.total, i.sold + i.blocked
			from rate_plan rp join room_type t on t.id = rp.room_type_id
			join rate r on r.rate_plan_id = rp.id and r.date >= $2::date and r.date < $2::date + $3::int and not r.closed
			join inventory i on i.room_type_id = rp.room_type_id and i.date = r.date
			where rp.property_id = $1 and i.total > 0
			and not exists (select 1 from price_review v where v.rate_plan_id = rp.id and v.date = r.date and v.reviewed_at > $4::timestamptz - interval '7 days')
			order by r.date, t.name, rp.name`, a.PropertyID, today, days, now)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s suggestionOut
			var date time.Time
			var total, taken int
			if err := rows.Scan(&s.RatePlanID, &s.RatePlanName, &s.RoomTypeID, &s.RoomType, &s.Date, &date, &s.Current, &total, &taken); err != nil {
				return err
			}
			s.Occupancy = float64(taken) / float64(total)
			in := Input{Current: s.Current, Occupancy: s.Occupancy, DaysAhead: int(date.Sub(today).Hours() / 24), Currency: currency}
			if m, ok := Median(prices[s.Date]); ok {
				in.Competitor, s.Competitor = &m, &m
			}
			if l, ok := limits[s.RoomTypeID]; ok {
				in.Floor, in.Ceiling = l.floor, l.ceiling
			}
			if sg, ok := Suggest(in); ok {
				s.Price, s.Reasons = sg.Price, sg.Reasons
				out = append(out, s)
			}
		}
		return rows.Err()
	})
	if err != nil {
		fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *handler) apply(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	var in struct {
		Changes []struct {
			RatePlanID uuid.UUID `json:"ratePlanId"`
			Date       string    `json:"date"`
			Price      int64     `json:"price"`
		} `json:"changes"`
	}
	if !read(w, r, &in) {
		return
	}
	if len(in.Changes) == 0 || len(in.Changes) > 400 {
		fail(w, errBad)
		return
	}
	now := h.Clock()
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		limits, err := loadLimits(tx, r, a.PropertyID)
		if err != nil {
			return err
		}
		for _, c := range in.Changes {
			if _, e := time.Parse("2006-01-02", c.Date); e != nil || c.Price < 1 || c.Price > 1e11 {
				return errBad
			}
			var rt uuid.UUID
			if err := tx.QueryRow(r.Context(), `select room_type_id from rate_plan where id = $1 and property_id = $2`, c.RatePlanID, a.PropertyID).Scan(&rt); err != nil {
				return err
			}
			if l, ok := limits[rt]; ok && ((l.floor != nil && c.Price < *l.floor) || (l.ceiling != nil && c.Price > *l.ceiling)) {
				return errOutside
			}
			tag, err := tx.Exec(r.Context(), `update rate set price = $3 where rate_plan_id = $1 and date = $2`, c.RatePlanID, c.Date, c.Price)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return pgx.ErrNoRows
			}
			if _, err := tx.Exec(r.Context(), `insert into price_review (organization_id, rate_plan_id, date, reviewed_at) values ($1, $2, $3, $4)
				on conflict (rate_plan_id, date) do update set reviewed_at = excluded.reviewed_at`, a.OrgID, c.RatePlanID, c.Date, now); err != nil {
				return err
			}
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "price_suggestions.applied", TargetType: "property", TargetID: a.PropertyID, After: in.Changes})
	})
	if err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) getLimits(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	type row struct {
		RoomTypeID uuid.UUID `json:"roomTypeId"`
		Floor      *int64    `json:"floor"`
		Ceiling    *int64    `json:"ceiling"`
	}
	out := []row{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select room_type_id, floor_price, ceiling_price from price_limit where property_id = $1`, a.PropertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x row
			if err := rows.Scan(&x.RoomTypeID, &x.Floor, &x.Ceiling); err != nil {
				return err
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	if err != nil {
		fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *handler) putLimit(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	var in struct {
		RoomTypeID uuid.UUID `json:"roomTypeId"`
		Floor      *int64    `json:"floor"`
		Ceiling    *int64    `json:"ceiling"`
	}
	if !read(w, r, &in) {
		return
	}
	if (in.Floor != nil && *in.Floor < 1) || (in.Ceiling != nil && *in.Ceiling < 1) || (in.Floor != nil && in.Ceiling != nil && *in.Floor > *in.Ceiling) {
		fail(w, errBad)
		return
	}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(r.Context(), `select count(*) from room_type where id = $1 and property_id = $2`, in.RoomTypeID, a.PropertyID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return pgx.ErrNoRows
		}
		if in.Floor == nil && in.Ceiling == nil {
			_, err := tx.Exec(r.Context(), `delete from price_limit where room_type_id = $1`, in.RoomTypeID)
			return err
		}
		_, err := tx.Exec(r.Context(), `insert into price_limit (organization_id, property_id, room_type_id, floor_price, ceiling_price) values ($1, $2, $3, $4, $5)
			on conflict (room_type_id) do update set floor_price = excluded.floor_price, ceiling_price = excluded.ceiling_price`, a.OrgID, a.PropertyID, in.RoomTypeID, in.Floor, in.Ceiling)
		if err != nil {
			return err
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "price_limit.changed", TargetType: "room_type", TargetID: in.RoomTypeID, After: in})
	})
	if err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) listCompetitors(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	from, e1 := time.Parse("2006-01-02", r.URL.Query().Get("from"))
	to, e2 := time.Parse("2006-01-02", r.URL.Query().Get("to"))
	if e1 != nil || e2 != nil || to.Before(from) || to.Sub(from) > 400*24*time.Hour {
		fail(w, errBad)
		return
	}
	type row struct {
		ID    uuid.UUID `json:"id"`
		Date  string    `json:"date"`
		Name  string    `json:"name"`
		Price int64     `json:"price"`
	}
	out := []row{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select id, to_char(date, 'YYYY-MM-DD'), name, price from competitor_rate where property_id = $1 and date between $2 and $3 order by date, name`, a.PropertyID, from, to)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x row
			if err := rows.Scan(&x.ID, &x.Date, &x.Name, &x.Price); err != nil {
				return err
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	if err != nil {
		fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *handler) postCompetitors(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	var in struct {
		Rates []struct {
			Date  string `json:"date"`
			Name  string `json:"name"`
			Price int64  `json:"price"`
		} `json:"rates"`
	}
	if !read(w, r, &in) {
		return
	}
	if len(in.Rates) == 0 || len(in.Rates) > 400 {
		fail(w, errBad)
		return
	}
	for i := range in.Rates {
		x := &in.Rates[i]
		x.Name = strings.TrimSpace(x.Name)
		if _, e := time.Parse("2006-01-02", x.Date); e != nil || x.Name == "" || utf8.RuneCountInString(x.Name) > 60 || x.Price < 1 || x.Price > 1e11 {
			fail(w, errBad)
			return
		}
	}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		for _, x := range in.Rates {
			if _, err := tx.Exec(r.Context(), `insert into competitor_rate (id, organization_id, property_id, date, name, price) values ($1, $2, $3, $4, $5, $6)
				on conflict (property_id, date, name) do update set price = excluded.price`, ids.New(), a.OrgID, a.PropertyID, x.Date, x.Name, x.Price); err != nil {
				return err
			}
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "competitor_rates.saved", TargetType: "property", TargetID: a.PropertyID, After: map[string]any{"count": len(in.Rates)}})
	})
	if err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) deleteCompetitor(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		fail(w, pgx.ErrNoRows)
		return
	}
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `delete from competitor_rate where id = $1 and property_id = $2`, id, a.PropertyID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
