// Package yieldrules lets a hotel set rules that change what is sent to the booking sites: close a room type when few
// are left, limit sales, cap what one site can sell, and move the price with demand. The rules are plain and
// predictable. They never touch the direct booking page.
package yieldrules

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/ids"
)

// Rule is one yield rule. An empty RoomTypeID means every room type, and empty dates mean every night.
type Rule struct {
	ID         uuid.UUID  `json:"id"`
	Kind       string     `json:"kind"` // auto_close, sell_limit, availability_cap or rate_adjust
	RoomTypeID *uuid.UUID `json:"roomTypeId"`
	From       string     `json:"from"`
	To         string     `json:"to"`
	Value      int        `json:"value"`   // rooms: the number left, taken or allowed, depending on the kind
	Percent    int        `json:"percent"` // rate_adjust only: how much the price moves
	Active     bool       `json:"active"`
}

// Night is one room type on one night, as the booking sites will see it.
type Night struct {
	Date       string
	RoomTypeID uuid.UUID
	Total      int // rooms of the type
	Taken      int // sold or blocked
	Available  int
	Price      int64 // minor units
}

// Valid says whether the rule makes sense.
func (r Rule) Valid() bool {
	date := func(s string) bool {
		if s == "" {
			return true
		}
		_, err := time.Parse("2006-01-02", s)
		return err == nil
	}
	if !date(r.From) || !date(r.To) || (r.From != "" && r.To != "" && r.From > r.To) {
		return false
	}
	switch r.Kind {
	case "auto_close":
		return r.Value >= 1 && r.Percent == 0
	case "sell_limit", "availability_cap":
		return r.Value >= 0 && r.Percent == 0
	case "rate_adjust":
		return r.Value >= 0 && r.Percent != 0 && r.Percent >= -50 && r.Percent <= 100
	}
	return false
}

func (r Rule) covers(n Night) bool {
	if !r.Active || (r.RoomTypeID != nil && *r.RoomTypeID != n.RoomTypeID) {
		return false
	}
	return (r.From == "" || n.Date >= r.From) && (r.To == "" || n.Date <= r.To)
}

// Apply returns the night as the booking sites should see it. Every rule reads the night as it was before any rule ran,
// so the order of the rules does not matter.
func Apply(n Night, rules []Rule) Night {
	out := n
	for _, r := range rules {
		if !r.covers(n) {
			continue
		}
		switch r.Kind {
		case "availability_cap":
			out.Available = min(out.Available, r.Value)
		case "sell_limit":
			out.Available = min(out.Available, max(0, r.Value-n.Taken))
		case "auto_close":
			if n.Available < r.Value {
				out.Available = 0
			}
		case "rate_adjust":
			if n.Available <= r.Value {
				out.Price = (out.Price*int64(100+r.Percent) + 50) / 100
			}
		}
	}
	return out
}

// Load reads the property's rules. Call it inside the organization's transaction.
func Load(ctx context.Context, tx pgx.Tx, prop uuid.UUID) ([]Rule, error) {
	rows, err := tx.Query(ctx, `select id, kind, room_type_id, coalesce(to_char(from_date, 'YYYY-MM-DD'), ''), coalesce(to_char(to_date, 'YYYY-MM-DD'), ''), value, percent, active
		from yield_rule where property_id = $1 order by created_at, id`, prop)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		var r Rule
		if err := rows.Scan(&r.ID, &r.Kind, &r.RoomTypeID, &r.From, &r.To, &r.Value, &r.Percent, &r.Active); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type handler struct{ httpx.Deps }

// Mount adds the routes. People who set rates manage the rules.
func Mount(mux *http.ServeMux, d httpx.Deps) {
	h := &handler{d}
	wrap := func(f http.HandlerFunc) http.HandlerFunc { return httpx.Wrap(access.Require(d, access.RatesEdit)(f)) }
	mux.HandleFunc("/api/yield-rules", httpx.Methods(map[string]http.HandlerFunc{http.MethodGet: wrap(h.list), http.MethodPost: wrap(h.create)}))
	mux.HandleFunc("/api/yield-rules/{id}", httpx.Methods(map[string]http.HandlerFunc{http.MethodPut: wrap(h.put), http.MethodDelete: wrap(h.remove)}))
}

func scope(w http.ResponseWriter, r *http.Request) (access.Actor, bool) {
	a := access.ActorFrom(r.Context())
	if a.PropertyID == uuid.Nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Choose a property first.")
		return a, false
	}
	return a, true
}

func fail(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
	default:
		httpx.Error(w, http.StatusInternalServerError, "internal", "Something went wrong.")
	}
	return true
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	var out []Rule
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var e error
		out, e = Load(r.Context(), tx, a.PropertyID)
		return e
	})
	if fail(w, err) {
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func readRule(w http.ResponseWriter, r *http.Request) (Rule, bool) {
	var in Rule
	dec := jsonDecoder(r)
	if err := dec.Decode(&in); err != nil || !in.Valid() {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Check the rule and try again.")
		return in, false
	}
	return in, true
}

func nullDate(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	in, ok := readRule(w, r)
	if !ok {
		return
	}
	in.ID = ids.New()
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		if in.RoomTypeID != nil {
			var n int
			if err := tx.QueryRow(r.Context(), `select count(*) from room_type where id = $1 and property_id = $2`, *in.RoomTypeID, a.PropertyID).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return pgx.ErrNoRows
			}
		}
		if _, err := tx.Exec(r.Context(), `insert into yield_rule (id, organization_id, property_id, kind, room_type_id, from_date, to_date, value, percent, active)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`, in.ID, a.OrgID, a.PropertyID, in.Kind, in.RoomTypeID, nullDate(in.From), nullDate(in.To), in.Value, in.Percent, in.Active); err != nil {
			return err
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "yield_rule.created", TargetType: "yield_rule", TargetID: in.ID, After: in})
	})
	if fail(w, err) {
		return
	}
	httpx.JSON(w, http.StatusCreated, in)
}

func (h *handler) put(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
		return
	}
	in, ok := readRule(w, r)
	if !ok {
		return
	}
	in.ID = id
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `update yield_rule set kind = $3, room_type_id = $4, from_date = $5, to_date = $6, value = $7, percent = $8, active = $9
			where id = $1 and property_id = $2`, id, a.PropertyID, in.Kind, in.RoomTypeID, nullDate(in.From), nullDate(in.To), in.Value, in.Percent, in.Active)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "yield_rule.changed", TargetType: "yield_rule", TargetID: id, After: in})
	})
	if fail(w, err) {
		return
	}
	httpx.JSON(w, http.StatusOK, in)
}

func (h *handler) remove(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
		return
	}
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `delete from yield_rule where id = $1 and property_id = $2`, id, a.PropertyID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "yield_rule.removed", TargetType: "yield_rule", TargetID: id})
	})
	if fail(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
