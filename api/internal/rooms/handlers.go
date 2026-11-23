package rooms

import (
	"encoding/json"
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
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/ids"
)

type handler struct{ httpx.Deps }

func Mount(mux *http.ServeMux, d httpx.Deps) {
	h := &handler{d}
	route := func(method string, f http.HandlerFunc, ps ...access.Permission) http.HandlerFunc {
		return httpx.Methods(map[string]http.HandlerFunc{method: httpx.Wrap(access.RequireAny(d, ps...)(f))})
	}
	both := func(get, post http.HandlerFunc, read, write []access.Permission) http.HandlerFunc {
		return httpx.Methods(map[string]http.HandlerFunc{
			http.MethodGet:  httpx.Wrap(access.RequireAny(d, read...)(get)),
			http.MethodPost: httpx.Wrap(access.RequireAny(d, write...)(post)),
		})
	}
	readers := []access.Permission{access.ReservationsView, access.HousekeepingView, access.RatesEdit}
	manage := []access.Permission{access.UsersManage}
	rates := []access.Permission{access.RatesEdit}
	mux.HandleFunc("/api/room-types", both(h.listRoomTypes, h.createRoomType, readers, manage))
	mux.HandleFunc("/api/room-types/{id}", route(http.MethodPatch, h.patchRoomType, manage...))
	mux.HandleFunc("/api/rooms", both(h.listRooms, h.createRoom, readers, manage))
	mux.HandleFunc("/api/rate-plans", both(h.listRatePlans, h.createRatePlan, []access.Permission{access.ReservationsView, access.RatesEdit}, rates))
	mux.HandleFunc("/api/rates", httpx.Methods(map[string]http.HandlerFunc{
		http.MethodGet: httpx.Wrap(access.RequireAny(d, access.ReservationsView, access.RatesEdit)(http.HandlerFunc(h.getRates))),
		http.MethodPut: httpx.Wrap(access.RequireAny(d, access.RatesEdit)(http.HandlerFunc(h.putRates))),
	}))
}

func bad(w http.ResponseWriter, msg string) {
	httpx.Error(w, http.StatusBadRequest, "bad_request", msg)
}
func notFound(w http.ResponseWriter) {
	httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
}

var errNotFound = errors.New("rooms: not found")

// scope returns the actor and the property the request is about, or answers 400 when no property was named.
func scope(w http.ResponseWriter, r *http.Request) (access.Actor, bool) {
	a := access.ActorFrom(r.Context())
	if a.PropertyID == uuid.Nil {
		bad(w, "Choose a property first.")
		return a, false
	}
	return a, true
}

func finish(w http.ResponseWriter, err error, ok func()) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, errNotFound), errors.Is(err, pgx.ErrNoRows):
		notFound(w)
	case errors.As(err, &pg) && pg.Code == "23505" && strings.Contains(pg.ConstraintName, "number"):
		httpx.Error(w, http.StatusConflict, "room_number_taken", "A room with that number already exists.")
	case errors.As(err, &pg) && pg.Code == "23505":
		httpx.Error(w, http.StatusConflict, "name_taken", "That name is already used.")
	case err != nil:
		httpx.InternalError(w, err)
	default:
		ok()
	}
}

type roomTypeIn struct {
	Name            string `json:"name"`
	BaseGuests      int    `json:"baseGuests"`
	MaxGuests       int    `json:"maxGuests"`
	ExtraAdultPrice int64  `json:"extraAdultPrice"`
	ExtraChildPrice int64  `json:"extraChildPrice"`
}

func (in *roomTypeIn) valid() bool {
	in.Name = strings.TrimSpace(in.Name)
	return in.Name != "" && len(in.Name) <= 100 && in.BaseGuests >= 1 && in.MaxGuests >= in.BaseGuests && in.MaxGuests <= 20 && in.ExtraAdultPrice >= 0 && in.ExtraChildPrice >= 0
}

type roomTypeOut struct {
	ID              uuid.UUID `json:"id"`
	Name            string    `json:"name"`
	BaseGuests      int       `json:"baseGuests"`
	MaxGuests       int       `json:"maxGuests"`
	ExtraAdultPrice *int64    `json:"extraAdultPrice,omitempty"` // left out for roles that must not see prices
	ExtraChildPrice *int64    `json:"extraChildPrice,omitempty"`
	Rooms           int       `json:"rooms"`
}

func (h *handler) listRoomTypes(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	out := []roomTypeOut{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(),
			`select t.id, t.name, t.base_guests, t.max_guests, t.extra_adult_price, t.extra_child_price, (select count(*) from room where room_type_id = t.id)
			 from room_type t where t.property_id = $1 order by t.name`, a.PropertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t roomTypeOut
			var adult, child int64
			if err := rows.Scan(&t.ID, &t.Name, &t.BaseGuests, &t.MaxGuests, &adult, &child, &t.Rooms); err != nil {
				return err
			}
			if access.Can(a.Role, access.ReservationsView) || access.Can(a.Role, access.RatesEdit) {
				t.ExtraAdultPrice, t.ExtraChildPrice = &adult, &child
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	finish(w, err, func() { httpx.JSON(w, http.StatusOK, out) })
}

func (h *handler) createRoomType(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	var in roomTypeIn
	if !httpx.Decode(w, r, &in) {
		return
	}
	if !in.valid() {
		bad(w, "Give a name, a base number of guests, and a maximum that is not lower.")
		return
	}
	id := ids.New()
	ctx := r.Context()
	err := db.WithOrg(ctx, h.Pool, a.OrgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`insert into room_type (id, organization_id, property_id, name, base_guests, max_guests, extra_adult_price, extra_child_price, created_at)
			 values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, id, a.OrgID, a.PropertyID, in.Name, in.BaseGuests, in.MaxGuests, in.ExtraAdultPrice, in.ExtraChildPrice, h.Clock()); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Action: "room_type.created", TargetType: "room_type", TargetID: id, TargetLabel: in.Name, After: in})
	})
	finish(w, err, func() { httpx.JSON(w, http.StatusCreated, map[string]any{"id": id}) })
}

func (h *handler) patchRoomType(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		notFound(w)
		return
	}
	var raw map[string]json.RawMessage
	if !httpx.Decode(w, r, &raw) {
		return
	}
	ctx := r.Context()
	err = db.WithOrg(ctx, h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var before roomTypeIn
		if err := tx.QueryRow(ctx, `select name, base_guests, max_guests, extra_adult_price, extra_child_price from room_type where id = $1 and property_id = $2`, id, a.PropertyID).
			Scan(&before.Name, &before.BaseGuests, &before.MaxGuests, &before.ExtraAdultPrice, &before.ExtraChildPrice); err != nil {
			return errNotFound
		}
		// A field left out keeps its value.
		merged := before
		if v, ok := raw["name"]; ok {
			_ = json.Unmarshal(v, &merged.Name)
		}
		for key, dst := range map[string]*int{"baseGuests": &merged.BaseGuests, "maxGuests": &merged.MaxGuests} {
			if v, ok := raw[key]; ok {
				_ = json.Unmarshal(v, dst)
			}
		}
		for key, dst := range map[string]*int64{"extraAdultPrice": &merged.ExtraAdultPrice, "extraChildPrice": &merged.ExtraChildPrice} {
			if v, ok := raw[key]; ok {
				_ = json.Unmarshal(v, dst)
			}
		}
		if !merged.valid() {
			return errBad
		}
		if _, err := tx.Exec(ctx, `update room_type set name = $2, base_guests = $3, max_guests = $4, extra_adult_price = $5, extra_child_price = $6 where id = $1`,
			id, merged.Name, merged.BaseGuests, merged.MaxGuests, merged.ExtraAdultPrice, merged.ExtraChildPrice); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Action: "room_type.changed", TargetType: "room_type", TargetID: id, TargetLabel: merged.Name, Before: before, After: merged})
	})
	if errors.Is(err, errBad) {
		bad(w, "Give a name, a base number of guests, and a maximum that is not lower.")
		return
	}
	finish(w, err, func() { w.WriteHeader(http.StatusNoContent) })
}

var errBad = errors.New("rooms: bad input")

type roomIn struct {
	RoomTypeID uuid.UUID `json:"roomTypeId"`
	Number     string    `json:"number"`
	Floor      int       `json:"floor"`
}

func (h *handler) createRoom(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	var in roomIn
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Number = strings.TrimSpace(in.Number)
	if in.Number == "" || len(in.Number) > 20 || in.RoomTypeID == uuid.Nil {
		bad(w, "Give a room number and a room type.")
		return
	}
	id := ids.New()
	ctx := r.Context()
	err := db.WithOrg(ctx, h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var one int
		if err := tx.QueryRow(ctx, `select 1 from room_type where id = $1 and property_id = $2`, in.RoomTypeID, a.PropertyID).Scan(&one); err != nil {
			return errNotFound
		}
		var today time.Time
		if err := tx.QueryRow(ctx, `select ($1::timestamptz at time zone time_zone)::date from property where id = $2`, h.Clock(), a.PropertyID).Scan(&today); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `insert into room (id, organization_id, property_id, room_type_id, number, floor, created_at) values ($1, $2, $3, $4, $5, $6, $7)`,
			id, a.OrgID, a.PropertyID, in.RoomTypeID, in.Number, in.Floor, h.Clock()); err != nil {
			return err
		}
		if err := EnsureInventory(ctx, tx, a.OrgID, in.RoomTypeID, today); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Action: "room.created", TargetType: "room", TargetID: id, TargetLabel: in.Number, After: in})
	})
	finish(w, err, func() { httpx.JSON(w, http.StatusCreated, map[string]any{"id": id}) })
}

func (h *handler) listRooms(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	type row struct {
		ID         uuid.UUID `json:"id"`
		Number     string    `json:"number"`
		Floor      int       `json:"floor"`
		Status     string    `json:"status"`
		RoomTypeID uuid.UUID `json:"roomTypeId"`
		TypeName   string    `json:"typeName"`
	}
	out := []row{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(),
			`select r.id, r.number, r.floor, r.status, r.room_type_id, t.name from room r join room_type t on t.id = r.room_type_id
			 where r.property_id = $1 order by r.floor, r.number`, a.PropertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x row
			if err := rows.Scan(&x.ID, &x.Number, &x.Floor, &x.Status, &x.RoomTypeID, &x.TypeName); err != nil {
				return err
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	finish(w, err, func() { httpx.JSON(w, http.StatusOK, out) })
}

type ratePlanIn struct {
	RoomTypeID          uuid.UUID `json:"roomTypeId"`
	Name                string    `json:"name"`
	FreeCancelUntilDays *int      `json:"freeCancelUntilDays"`
	FreeCancelUntilTime *string   `json:"freeCancelUntilTime"`
	LateCancelPenalty   string    `json:"lateCancelPenalty"`
	LateCancelPercent   *int      `json:"lateCancelPercent"`
	MealPlan            string    `json:"mealPlan"`
}

func (h *handler) createRatePlan(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	var in ratePlanIn
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.LateCancelPenalty == "" {
		in.LateCancelPenalty = "none"
	}
	if in.MealPlan == "" {
		in.MealPlan = "none"
	}
	if in.Name == "" || in.RoomTypeID == uuid.Nil || (in.LateCancelPenalty == "percent") != (in.LateCancelPercent != nil) {
		bad(w, "Give a name and a room type. A percent penalty needs its percent.")
		return
	}
	id := ids.New()
	ctx := r.Context()
	err := db.WithOrg(ctx, h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var one int
		if err := tx.QueryRow(ctx, `select 1 from room_type where id = $1 and property_id = $2`, in.RoomTypeID, a.PropertyID).Scan(&one); err != nil {
			return errNotFound
		}
		if _, err := tx.Exec(ctx,
			`insert into rate_plan (id, organization_id, property_id, room_type_id, name, free_cancel_until_days, free_cancel_until_time, late_cancel_penalty, late_cancel_percent, meal_plan, created_at)
			 values ($1, $2, $3, $4, $5, $6, $7::time, $8, $9, $10, $11)`,
			id, a.OrgID, a.PropertyID, in.RoomTypeID, in.Name, in.FreeCancelUntilDays, in.FreeCancelUntilTime, in.LateCancelPenalty, in.LateCancelPercent, in.MealPlan, h.Clock()); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Action: "rate_plan.created", TargetType: "rate_plan", TargetID: id, TargetLabel: in.Name, After: in})
	})
	finish(w, err, func() { httpx.JSON(w, http.StatusCreated, map[string]any{"id": id}) })
}

func (h *handler) listRatePlans(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	type row struct {
		ID                  uuid.UUID `json:"id"`
		RoomTypeID          uuid.UUID `json:"roomTypeId"`
		Name                string    `json:"name"`
		FreeCancelUntilDays *int      `json:"freeCancelUntilDays"`
		LateCancelPenalty   string    `json:"lateCancelPenalty"`
		MealPlan            string    `json:"mealPlan"`
	}
	out := []row{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select id, room_type_id, name, free_cancel_until_days, late_cancel_penalty, meal_plan from rate_plan where property_id = $1 order by name`, a.PropertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x row
			if err := rows.Scan(&x.ID, &x.RoomTypeID, &x.Name, &x.FreeCancelUntilDays, &x.LateCancelPenalty, &x.MealPlan); err != nil {
				return err
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	finish(w, err, func() { httpx.JSON(w, http.StatusOK, out) })
}

const day = "2006-01-02"

type rateIn struct {
	RatePlanID        uuid.UUID `json:"ratePlanId"`
	From              string    `json:"from"`
	To                string    `json:"to"`
	Price             int64     `json:"price"`
	MinNights         int       `json:"minNights"`
	Closed            bool      `json:"closed"`
	ClosedToArrival   bool      `json:"closedToArrival"`
	ClosedToDeparture bool      `json:"closedToDeparture"`
}

// putRates sets one price and its rules for every date in [from, to). A price far from the room type's usual is
// still saved, and its dates come back in "suspicious" so the screen can ask the person to confirm.
func (h *handler) putRates(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	var in rateIn
	if !httpx.Decode(w, r, &in) {
		return
	}
	from, e1 := time.Parse(day, in.From)
	to, e2 := time.Parse(day, in.To)
	if in.MinNights == 0 {
		in.MinNights = 1
	}
	if e1 != nil || e2 != nil || !to.After(from) || to.Sub(from) > 400*24*time.Hour || in.Price < 0 || in.MinNights < 1 {
		bad(w, "Give a first date, a later last date within 400 days, a price of zero or more, and at least 1 night.")
		return
	}
	ctx := r.Context()
	suspicious := []string{}
	err := db.WithOrg(ctx, h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var roomTypeID uuid.UUID
		if err := tx.QueryRow(ctx, `select room_type_id from rate_plan where id = $1 and property_id = $2`, in.RatePlanID, a.PropertyID).Scan(&roomTypeID); err != nil {
			return errNotFound
		}
		var usual *float64
		if err := tx.QueryRow(ctx,
			`select avg(r.price)::float8 from rate r join rate_plan p on p.id = r.rate_plan_id where p.room_type_id = $1`, roomTypeID).Scan(&usual); err != nil {
			return err
		}
		flag := usual != nil && (float64(in.Price) < *usual/2 || float64(in.Price) > *usual*3)
		if _, err := tx.Exec(ctx,
			`insert into rate (rate_plan_id, date, organization_id, price, min_nights, closed, closed_to_arrival, closed_to_departure, suspicious)
			 select $1, d::date, $2, $3, $4, $5, $6, $7, $8 from generate_series($9::date, $10::date - 1, interval '1 day') d
			 on conflict (rate_plan_id, date) do update set price = excluded.price, min_nights = excluded.min_nights, closed = excluded.closed,
			   closed_to_arrival = excluded.closed_to_arrival, closed_to_departure = excluded.closed_to_departure, suspicious = excluded.suspicious`,
			in.RatePlanID, a.OrgID, in.Price, in.MinNights, in.Closed, in.ClosedToArrival, in.ClosedToDeparture, flag, from, to); err != nil {
			return err
		}
		if flag {
			for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
				suspicious = append(suspicious, d.Format(day))
			}
		}
		return audit.Record(ctx, tx, audit.Entry{Action: "rates.changed", TargetType: "rate_plan", TargetID: in.RatePlanID, After: in})
	})
	finish(w, err, func() { httpx.JSON(w, http.StatusOK, map[string]any{"suspicious": suspicious}) })
}

func (h *handler) getRates(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	plan, perr := uuid.Parse(r.URL.Query().Get("ratePlanId"))
	from, e1 := time.Parse(day, r.URL.Query().Get("from"))
	to, e2 := time.Parse(day, r.URL.Query().Get("to"))
	if perr != nil || e1 != nil || e2 != nil || !to.After(from) || to.Sub(from) > 400*24*time.Hour {
		bad(w, "Give a rate plan and a date range of up to 400 days.")
		return
	}
	type row struct {
		Date              string `json:"date"`
		Price             int64  `json:"price"`
		MinNights         int    `json:"minNights"`
		Closed            bool   `json:"closed"`
		ClosedToArrival   bool   `json:"closedToArrival"`
		ClosedToDeparture bool   `json:"closedToDeparture"`
		Suspicious        bool   `json:"suspicious"`
	}
	out := []row{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(),
			`select to_char(r.date, 'YYYY-MM-DD'), r.price, r.min_nights, r.closed, r.closed_to_arrival, r.closed_to_departure, r.suspicious
			 from rate r join rate_plan p on p.id = r.rate_plan_id where r.rate_plan_id = $1 and p.property_id = $2 and r.date >= $3 and r.date < $4 order by r.date`,
			plan, a.PropertyID, from, to)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x row
			if err := rows.Scan(&x.Date, &x.Price, &x.MinNights, &x.Closed, &x.ClosedToArrival, &x.ClosedToDeparture, &x.Suspicious); err != nil {
				return err
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	finish(w, err, func() { httpx.JSON(w, http.StatusOK, out) })
}
