// Package promo holds the promo codes a hotel gives out for stays booked on its own booking page. A code takes a percent
// or a fixed amount off the room price, and the discount is posted to the guest's folio as an adjustment.
package promo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/folio"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/ids"
)

// ErrInvalid means the code does not work for this stay: it is unknown, switched off, used up, or the stay does not fit it.
var ErrInvalid = errors.New("promo: the code does not work for this stay")

var codeShape = regexp.MustCompile(`^[A-Z0-9_-]{3,20}$`)

// Discount is what a code takes off a room price. A percent is rounded to the nearest unit, and an amount never
// takes more than the price.
func Discount(kind string, value, total int64) int64 {
	if kind == "percent" {
		return (total*value + 50) / 100
	}
	return min(value, total)
}

// Apply checks the code against the stay, counts one use and posts the discount to the reservation's folio. It runs
// in the booking's transaction, so a booking that fails gives the use back. Codes are not case sensitive.
func Apply(ctx context.Context, tx pgx.Tx, org, prop, reservation uuid.UUID, code string, total int64, nights int, arrival time.Time, now time.Time) error {
	code = strings.ToUpper(strings.TrimSpace(code))
	var id uuid.UUID
	var kind string
	var value int64
	err := tx.QueryRow(ctx, `update promo_code set used = used + 1
		where property_id = $1 and code = $2 and active and (max_uses is null or used < max_uses) and min_nights <= $3
		and (arrive_from is null or arrive_from <= $4::date) and (arrive_to is null or arrive_to >= $4::date)
		returning id, kind, value`, prop, code, nights, arrival).Scan(&id, &kind, &value)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalid
	}
	if err != nil {
		return err
	}
	off := Discount(kind, value, total)
	if off <= 0 {
		return nil
	}
	f, _, err := folio.Ensure(ctx, tx, org, prop, reservation, now)
	if err != nil {
		return err
	}
	_, err = folio.AddLine(ctx, tx, org, prop, f, uuid.Nil, "adjustment", "Promo "+code, 1, -off, now)
	return err
}

// Code is one promo code as staff see it.
type Code struct {
	ID         uuid.UUID `json:"id"`
	Code       string    `json:"code"`
	Kind       string    `json:"kind"` // percent or amount
	Value      int64     `json:"value"`
	MinNights  int       `json:"minNights"`
	ArriveFrom string    `json:"arriveFrom"`
	ArriveTo   string    `json:"arriveTo"`
	MaxUses    *int      `json:"maxUses"`
	Used       int       `json:"used"`
	Active     bool      `json:"active"`
}

func (c Code) valid() bool {
	date := func(s string) bool {
		if s == "" {
			return true
		}
		_, err := time.Parse("2006-01-02", s)
		return err == nil
	}
	return codeShape.MatchString(c.Code) && (c.Kind == "percent" && c.Value >= 1 && c.Value <= 100 || c.Kind == "amount" && c.Value >= 1 && c.Value <= 1e9) &&
		c.MinNights >= 1 && c.MinNights <= 365 && date(c.ArriveFrom) && date(c.ArriveTo) && (c.ArriveFrom == "" || c.ArriveTo == "" || c.ArriveFrom <= c.ArriveTo) &&
		(c.MaxUses == nil || *c.MaxUses >= 1)
}

type handler struct{ httpx.Deps }

// Mount adds the routes. People who set rates manage the codes.
func Mount(mux *http.ServeMux, d httpx.Deps) {
	h := &handler{d}
	wrap := func(f http.HandlerFunc) http.HandlerFunc { return httpx.Wrap(access.Require(d, access.RatesEdit)(f)) }
	mux.HandleFunc("/api/promo-codes", httpx.Methods(map[string]http.HandlerFunc{http.MethodGet: wrap(h.list), http.MethodPost: wrap(h.create)}))
	mux.HandleFunc("/api/promo-codes/{id}", httpx.Methods(map[string]http.HandlerFunc{http.MethodPut: wrap(h.put), http.MethodDelete: wrap(h.remove)}))
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
	var pg *pgconn.PgError
	switch {
	case err == nil:
		return false
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
	case errors.As(err, &pg) && pg.Code == "23505":
		httpx.Error(w, http.StatusConflict, "duplicate", "That code already exists.")
	default:
		httpx.Error(w, http.StatusInternalServerError, "internal", "Something went wrong.")
	}
	return true
}

func nullDate(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func read(w http.ResponseWriter, r *http.Request) (Code, bool) {
	var in Code
	d := json.NewDecoder(io.LimitReader(r.Body, 4<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Check the code and try again.")
		return in, false
	}
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	if in.MinNights == 0 {
		in.MinNights = 1
	}
	if !in.valid() {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Check the code and try again.")
		return in, false
	}
	return in, true
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	out := []Code{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select id, code, kind, value, min_nights, coalesce(to_char(arrive_from, 'YYYY-MM-DD'), ''), coalesce(to_char(arrive_to, 'YYYY-MM-DD'), ''), max_uses, used, active
			from promo_code where property_id = $1 order by created_at, id`, a.PropertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c Code
			if err := rows.Scan(&c.ID, &c.Code, &c.Kind, &c.Value, &c.MinNights, &c.ArriveFrom, &c.ArriveTo, &c.MaxUses, &c.Used, &c.Active); err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	if fail(w, err) {
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	in, ok := read(w, r)
	if !ok {
		return
	}
	in.ID, in.Used = ids.New(), 0
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `insert into promo_code (id, organization_id, property_id, code, kind, value, min_nights, arrive_from, arrive_to, max_uses, active)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, in.ID, a.OrgID, a.PropertyID, in.Code, in.Kind, in.Value, in.MinNights, nullDate(in.ArriveFrom), nullDate(in.ArriveTo), in.MaxUses, in.Active); err != nil {
			return err
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "promo_code.created", TargetType: "promo_code", TargetID: in.ID, TargetLabel: in.Code, After: in})
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
	in, ok := read(w, r)
	if !ok {
		return
	}
	in.ID = id
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `update promo_code set code = $3, kind = $4, value = $5, min_nights = $6, arrive_from = $7, arrive_to = $8, max_uses = $9, active = $10
			where id = $1 and property_id = $2`, id, a.PropertyID, in.Code, in.Kind, in.Value, in.MinNights, nullDate(in.ArriveFrom), nullDate(in.ArriveTo), in.MaxUses, in.Active)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "promo_code.changed", TargetType: "promo_code", TargetID: id, TargetLabel: in.Code, After: in})
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
		tag, err := tx.Exec(r.Context(), `delete from promo_code where id = $1 and property_id = $2`, id, a.PropertyID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "promo_code.removed", TargetType: "promo_code", TargetID: id})
	})
	if fail(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
