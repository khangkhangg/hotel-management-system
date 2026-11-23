// Package extras is the catalog of what a hotel sells besides rooms, and selling one to a stay.
package extras

import (
	"context"
	"errors"
	"net/http"
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

type handler struct{ httpx.Deps }

// Mount adds the catalog routes. Owners and managers edit it, and anyone who can edit a reservation sells from it.
func Mount(mux *http.ServeMux, d httpx.Deps) {
	h := &handler{d}
	wrap := func(f http.HandlerFunc, ps ...access.Permission) http.HandlerFunc {
		return httpx.Wrap(access.RequireAny(d, ps...)(f))
	}
	sell := []access.Permission{access.ReservationsEdit, access.PaymentsTake}
	mux.HandleFunc("/api/extras", httpx.Methods(map[string]http.HandlerFunc{
		http.MethodGet:  wrap(h.list, append([]access.Permission{access.ExtrasManage}, sell...)...),
		http.MethodPost: wrap(h.create, access.ExtrasManage),
	}))
	mux.HandleFunc("/api/extras/{id}", httpx.Methods(map[string]http.HandlerFunc{http.MethodPatch: wrap(h.patch, access.ExtrasManage)}))
	mux.HandleFunc("/api/reservations/{id}/extras", httpx.Methods(map[string]http.HandlerFunc{http.MethodPost: wrap(h.sell, sell...)}))
}

type extraIn struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Unit  string `json:"unit"`
	Price int64  `json:"price"`
}

func (in *extraIn) valid() bool {
	in.Name = strings.TrimSpace(in.Name)
	return in.Name != "" && len(in.Name) <= 100 && in.Price >= 1 && in.Price <= 1e9 &&
		(in.Kind == "service" || in.Kind == "tour" || in.Kind == "product") &&
		(in.Unit == "each" || in.Unit == "per_night" || in.Unit == "per_person" || in.Unit == "per_person_night")
}

type extraOut struct {
	ID uuid.UUID `json:"id"`
	extraIn
	Active bool `json:"active"`
}

func notFound(w http.ResponseWriter) {
	httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
}

func internal(w http.ResponseWriter) {
	httpx.Error(w, http.StatusInternalServerError, "internal", "Something went wrong.")
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	// Only people who edit rates see switched-off extras, so staff selling never offers one.
	all := r.URL.Query().Get("all") == "1" && access.Can(a.Role, access.ExtrasManage)
	out := []extraOut{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select id, name, kind, unit, price, active from extra where property_id = $1 and (active or $2) order by name`, a.PropertyID, all)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e extraOut
			if err := rows.Scan(&e.ID, &e.Name, &e.Kind, &e.Unit, &e.Price, &e.Active); err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	if err != nil {
		internal(w)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func scope(w http.ResponseWriter, r *http.Request) (access.Actor, bool) {
	a := access.ActorFrom(r.Context())
	if a.PropertyID == uuid.Nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Choose a property first.")
		return a, false
	}
	return a, true
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	var in extraIn
	if !httpx.Decode(w, r, &in) {
		return
	}
	if !in.valid() {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Give a name, a type, a unit and a price above zero.")
		return
	}
	id := ids.New()
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `insert into extra (id, organization_id, property_id, name, kind, unit, price, created_at) values ($1, $2, $3, $4, $5, $6, $7, $8)`,
			id, a.OrgID, a.PropertyID, in.Name, in.Kind, in.Unit, in.Price, h.Clock()); err != nil {
			return err
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "extra.created", TargetType: "extra", TargetID: id, TargetLabel: in.Name, After: in})
	})
	var pg *pgconn.PgError
	switch {
	case errors.As(err, &pg) && pg.Code == "23505":
		httpx.Error(w, http.StatusConflict, "name_taken", "An extra with that name already exists.")
	case err != nil:
		internal(w)
	default:
		httpx.JSON(w, http.StatusCreated, map[string]any{"id": id})
	}
}

// patch changes the price or turns an extra on or off. A field left out keeps its value.
func (h *handler) patch(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		notFound(w)
		return
	}
	var in struct {
		Price  *int64 `json:"price"`
		Active *bool  `json:"active"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	if in.Price != nil && (*in.Price < 1 || *in.Price > 1e9) {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "The price must be above zero.")
		return
	}
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `update extra set price = coalesce($3, price), active = coalesce($4, active) where id = $1 and property_id = $2`, id, a.PropertyID, in.Price, in.Active)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "extra.changed", TargetType: "extra", TargetID: id, After: in})
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		notFound(w)
	case err != nil:
		internal(w)
	default:
		httpx.JSON(w, http.StatusOK, map[string]any{"id": id})
	}
}

// sell posts the extra to the stay's folio. The unit scales the quantity: nights and guests come from the stay.
func (h *handler) sell(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	res, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		notFound(w)
		return
	}
	var in struct {
		ExtraID  uuid.UUID `json:"extraId"`
		Quantity int       `json:"quantity"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	if in.Quantity < 1 || in.Quantity > 100 {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Choose how many, from 1 to 100.")
		return
	}
	var lineID uuid.UUID
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) (e error) {
		lineID, e = Add(r.Context(), tx, a.OrgID, a.PropertyID, res, in.ExtraID, in.Quantity, a.UserID, h.Clock())
		return e
	})
	switch {
	case errors.Is(err, folio.ErrBad):
		httpx.Error(w, http.StatusBadRequest, "bad_request", "That extra cannot be added to this stay in that amount.")
	case errors.Is(err, pgx.ErrNoRows), errors.Is(err, folio.ErrNotFound):
		notFound(w)
	case err != nil:
		internal(w)
	default:
		httpx.JSON(w, http.StatusCreated, map[string]any{"id": lineID})
	}
}

// Add posts an extra to a stay's folio. The unit scales the quantity: nights and guests come from the stay.
// A missing, switched-off or foreign extra, or a stay with no rooms, is pgx.ErrNoRows. A guest booking has no user, so by may be uuid.Nil.
func Add(ctx context.Context, tx pgx.Tx, org, prop, reservation, extra uuid.UUID, quantity int, by uuid.UUID, now time.Time) (uuid.UUID, error) {
	var name, unit string
	var price int64
	if err := tx.QueryRow(ctx, `select name, unit, price from extra where id = $1 and property_id = $2 and active`, extra, prop).Scan(&name, &unit, &price); err != nil {
		return uuid.Nil, err
	}
	var status string
	if err := tx.QueryRow(ctx, `select status from reservation where id = $1 and property_id = $2`, reservation, prop).Scan(&status); err != nil {
		return uuid.Nil, err
	}
	if status != "pending" && status != "confirmed" && status != "checked_in" { // a closed stay takes no new charges
		return uuid.Nil, folio.ErrBad
	}
	var nights, guests, guestNights int
	if err := tx.QueryRow(ctx, `select coalesce(max(upper(stay) - lower(stay)), 0), coalesce(sum(adults + children), 0), coalesce(sum((adults + children) * (upper(stay) - lower(stay))), 0)
		from reservation_room x join reservation r on r.id = x.reservation_id where r.id = $1 and r.property_id = $2`, reservation, prop).Scan(&nights, &guests, &guestNights); err != nil || nights == 0 {
		return uuid.Nil, pgx.ErrNoRows
	}
	switch unit {
	case "per_night":
		quantity *= nights
	case "per_person":
		quantity *= guests
	case "per_person_night":
		quantity *= guestNights
	}
	if quantity > 1000 {
		return uuid.Nil, folio.ErrBad
	}
	folioID, _, err := folio.Ensure(ctx, tx, org, prop, reservation, now)
	if err != nil {
		return uuid.Nil, err
	}
	return folio.AddLine(ctx, tx, org, prop, folioID, by, "extra", name, quantity, price, now)
}
