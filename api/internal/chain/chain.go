// Package chain holds what a group of hotels shares: rate plan templates that a chain sets once and each property links to.
package chain

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/ids"
)

type handler struct{ httpx.Deps }

var errForbidden = errors.New("chain: not organization-wide")

// Mount adds the template routes. Anyone who edits rates sees the templates and links a property to one,
// but only people whose membership covers the whole organization change them.
func Mount(mux *http.ServeMux, d httpx.Deps) {
	h := &handler{d}
	wrap := func(f http.HandlerFunc, ps ...access.Permission) http.HandlerFunc {
		return httpx.Wrap(access.RequireAny(d, ps...)(f))
	}
	mux.HandleFunc("/api/rate-plan-templates", httpx.Methods(map[string]http.HandlerFunc{
		http.MethodGet:  wrap(h.list, access.RatesEdit),
		http.MethodPost: wrap(h.create, access.RatesEdit),
	}))
	mux.HandleFunc("/api/rate-plan-templates/{id}", httpx.Methods(map[string]http.HandlerFunc{http.MethodPut: wrap(h.change, access.RatesEdit)}))
	mux.HandleFunc("/api/rate-plan-templates/{id}/link", httpx.Methods(map[string]http.HandlerFunc{http.MethodPost: wrap(h.link, access.RatesEdit)}))
}

type templateIn struct {
	Name                string  `json:"name"`
	FreeCancelUntilDays *int    `json:"freeCancelUntilDays"`
	FreeCancelUntilTime *string `json:"freeCancelUntilTime"`
	LateCancelPenalty   string  `json:"lateCancelPenalty"`
	LateCancelPercent   *int    `json:"lateCancelPercent"`
	MealPlan            string  `json:"mealPlan"`
}

var clock = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

func (in *templateIn) valid() bool {
	in.Name = strings.TrimSpace(in.Name)
	in.MealPlan = strings.TrimSpace(in.MealPlan)
	if in.MealPlan == "" {
		in.MealPlan = "none"
	}
	switch in.LateCancelPenalty {
	case "none", "first_night", "full":
	case "percent":
		if in.LateCancelPercent == nil || *in.LateCancelPercent < 1 || *in.LateCancelPercent > 100 {
			return false
		}
	default:
		return false
	}
	return in.Name != "" && len(in.Name) <= 100 && len(in.MealPlan) <= 40 &&
		(in.FreeCancelUntilDays == nil || (*in.FreeCancelUntilDays >= 0 && *in.FreeCancelUntilDays <= 365)) &&
		(in.FreeCancelUntilTime == nil || clock.MatchString(*in.FreeCancelUntilTime))
}

type templateOut struct {
	ID uuid.UUID `json:"id"`
	templateIn
}

func fail(w http.ResponseWriter, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, errForbidden):
		httpx.Error(w, http.StatusForbidden, "forbidden", "Only people who work across the whole organization can change the chain's plans.")
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
	case errors.As(err, &pg) && pg.Code == "23505":
		httpx.Error(w, http.StatusConflict, "name_taken", "That name is already used.")
	default:
		httpx.Error(w, http.StatusInternalServerError, "internal", "Something went wrong.")
	}
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	a := access.ActorFrom(r.Context())
	out := []templateOut{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select id, name, free_cancel_until_days, to_char(free_cancel_until_time, 'HH24:MI'), late_cancel_penalty, late_cancel_percent, meal_plan from rate_plan_template order by name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t templateOut
			if err := rows.Scan(&t.ID, &t.Name, &t.FreeCancelUntilDays, &t.FreeCancelUntilTime, &t.LateCancelPenalty, &t.LateCancelPercent, &t.MealPlan); err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	if err != nil {
		fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	a := access.ActorFrom(r.Context())
	var in templateIn
	if !httpx.Decode(w, r, &in) {
		return
	}
	if !in.valid() {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Give a name and a cancellation rule that makes sense.")
		return
	}
	id := ids.New()
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		if wide, err := access.OrgWide(r.Context(), tx, a); err != nil || !wide {
			return orElse(err, errForbidden)
		}
		if _, err := tx.Exec(r.Context(), `insert into rate_plan_template (id, organization_id, name, free_cancel_until_days, free_cancel_until_time, late_cancel_penalty, late_cancel_percent, meal_plan, created_at)
			values ($1, $2, $3, $4, $5::time, $6, $7, $8, $9)`, id, a.OrgID, in.Name, in.FreeCancelUntilDays, in.FreeCancelUntilTime, in.LateCancelPenalty, in.LateCancelPercent, in.MealPlan, h.Clock()); err != nil {
			return err
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "rate_plan_template.created", TargetType: "rate_plan_template", TargetID: id, TargetLabel: in.Name, After: in})
	})
	if err != nil {
		fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"id": id})
}

func orElse(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}

// change replaces a template and pushes the new rules to every rate plan linked to it.
func (h *handler) change(w http.ResponseWriter, r *http.Request) {
	a := access.ActorFrom(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		fail(w, pgx.ErrNoRows)
		return
	}
	var in templateIn
	if !httpx.Decode(w, r, &in) {
		return
	}
	if !in.valid() {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Give a name and a cancellation rule that makes sense.")
		return
	}
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		if wide, err := access.OrgWide(r.Context(), tx, a); err != nil || !wide {
			return orElse(err, errForbidden)
		}
		tag, err := tx.Exec(r.Context(), `update rate_plan_template set name = $2, free_cancel_until_days = $3, free_cancel_until_time = $4::time, late_cancel_penalty = $5, late_cancel_percent = $6, meal_plan = $7 where id = $1`,
			id, in.Name, in.FreeCancelUntilDays, in.FreeCancelUntilTime, in.LateCancelPenalty, in.LateCancelPercent, in.MealPlan)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		if _, err := tx.Exec(r.Context(), `update rate_plan set name = $2, free_cancel_until_days = $3, free_cancel_until_time = $4::time, late_cancel_penalty = $5, late_cancel_percent = $6, meal_plan = $7 where template_id = $1`,
			id, in.Name, in.FreeCancelUntilDays, in.FreeCancelUntilTime, in.LateCancelPenalty, in.LateCancelPercent, in.MealPlan); err != nil {
			return err
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "rate_plan_template.changed", TargetType: "rate_plan_template", TargetID: id, TargetLabel: in.Name, After: in})
	})
	if err != nil {
		fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"id": id})
}

// link makes a rate plan at this property, for one room type, from the template.
func (h *handler) link(w http.ResponseWriter, r *http.Request) {
	a := access.ActorFrom(r.Context())
	tpl, err := uuid.Parse(r.PathValue("id"))
	var in struct {
		RoomTypeID uuid.UUID `json:"roomTypeId"`
	}
	if err != nil {
		fail(w, pgx.ErrNoRows)
		return
	}
	if a.PropertyID == uuid.Nil || !httpx.Decode(w, r, &in) {
		if a.PropertyID == uuid.Nil {
			httpx.Error(w, http.StatusBadRequest, "bad_request", "Choose a property first.")
		}
		return
	}
	id := ids.New()
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var one int
		if err := tx.QueryRow(r.Context(), `select 1 from room_type where id = $1 and property_id = $2`, in.RoomTypeID, a.PropertyID).Scan(&one); err != nil {
			return err
		}
		var name string
		if err := tx.QueryRow(r.Context(), `insert into rate_plan (id, organization_id, property_id, room_type_id, template_id, name, free_cancel_until_days, free_cancel_until_time, late_cancel_penalty, late_cancel_percent, meal_plan, created_at)
			select $1, organization_id, $2, $3, id, name, free_cancel_until_days, free_cancel_until_time, late_cancel_penalty, late_cancel_percent, meal_plan, $5 from rate_plan_template where id = $4 returning name`,
			id, a.PropertyID, in.RoomTypeID, tpl, h.Clock()).Scan(&name); err != nil {
			return err
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "rate_plan.linked", TargetType: "rate_plan", TargetID: id, TargetLabel: name, After: map[string]any{"templateId": tpl, "roomTypeId": in.RoomTypeID}})
	})
	if err != nil {
		fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"id": id})
}
