package booking

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/rooms"
)

type handler struct{ httpx.Deps }

func Mount(mux *http.ServeMux, d httpx.Deps) {
	h := &handler{d}
	view, edit := access.ReservationsView, access.ReservationsEdit
	route := func(methods map[string]struct {
		f http.HandlerFunc
		p access.Permission
	}) http.HandlerFunc {
		m := map[string]http.HandlerFunc{}
		for method, e := range methods {
			m[method] = httpx.Wrap(access.Require(d, e.p)(e.f))
		}
		return httpx.Methods(m)
	}
	type e = struct {
		f http.HandlerFunc
		p access.Permission
	}
	mux.HandleFunc("/api/reservations", route(map[string]e{http.MethodGet: {h.list, view}, http.MethodPost: {h.create, edit}}))
	mux.HandleFunc("/api/reservations/{id}", route(map[string]e{http.MethodGet: {h.get, view}}))
	mux.HandleFunc("/api/reservations/{id}/cancel", route(map[string]e{http.MethodPost: {h.cancel, edit}}))
	mux.HandleFunc("/api/availability", route(map[string]e{http.MethodGet: {h.availability, view}}))
	mountStay(mux, h, d)
	mux.HandleFunc("/api/calendar", route(map[string]e{http.MethodGet: {h.calendar, view}}))
	mux.HandleFunc("/api/quote", route(map[string]e{http.MethodGet: {h.quote, view}}))
	mux.HandleFunc("/api/guests", route(map[string]e{http.MethodGet: {h.listGuests, view}}))
	mux.HandleFunc("/api/guests/{id}", route(map[string]e{http.MethodGet: {h.getGuest, view}, http.MethodPatch: {h.patchGuest, edit}}))
}

func bad(w http.ResponseWriter, code, msg string) { httpx.Error(w, http.StatusBadRequest, code, msg) }
func notFound(w http.ResponseWriter) {
	httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
}

// property returns the actor, answering 400 when no property was named.
func property(w http.ResponseWriter, r *http.Request) (access.Actor, bool) {
	a := access.ActorFrom(r.Context())
	if a.PropertyID == uuid.Nil {
		bad(w, "bad_request", "Choose a property first.")
		return a, false
	}
	return a, true
}

// fail maps an error to its answer; it returns false when err is nil.
func fail(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, ErrSoldOut):
		httpx.Error(w, http.StatusConflict, "sold_out", "There is no room left on one of those nights.")
	case errors.Is(err, ErrStayRule):
		bad(w, "stay_rule", "That stay breaks a rule on the rate: a closed night, a minimum stay, or no arrival or departure that day.")
	case errors.Is(err, ErrPast):
		bad(w, "past", "The stay cannot start before today.")
	case errors.Is(err, rooms.ErrNoRate):
		bad(w, "no_rate", "One of those nights has no price set.")
	case errors.Is(err, rooms.ErrTooManyGuests):
		bad(w, "too_many_guests", "That is more guests than the room holds.")
	case errors.Is(err, rooms.ErrNoAdult), errors.Is(err, ErrBad):
		bad(w, "bad_request", "Check the guest, the dates (1 to 60 nights) and the number of guests.")
	case errors.Is(err, ErrNotFound), errors.Is(err, pgx.ErrNoRows):
		notFound(w)
	case errors.Is(err, ErrWrongType):
		httpx.Error(w, http.StatusConflict, "wrong_type", "That room is not of the booked room type.")
	case errors.Is(err, ErrRoomNotReady):
		httpx.Error(w, http.StatusConflict, "room_not_ready", "That room is not ready. Mark it clean, or assign it anyway with a reason.")
	case errors.Is(err, ErrRoomTaken):
		httpx.Error(w, http.StatusConflict, "room_taken", "That room already holds another stay on those nights.")
	case errors.Is(err, ErrNoRoom):
		httpx.Error(w, http.StatusConflict, "no_room", "Assign a room before checking in.")
	case errors.Is(err, ErrTooEarly):
		httpx.Error(w, http.StatusConflict, "too_early", "That cannot be done before the arrival day.")
	case errors.Is(err, ErrAlreadyCheckedIn):
		httpx.Error(w, http.StatusConflict, "already_checked_in", "That guest is already checked in.")
	case errors.Is(err, ErrNotCheckedIn):
		httpx.Error(w, http.StatusConflict, "not_checked_in", "That guest is not checked in.")
	case errors.Is(err, ErrNotConfirmed):
		httpx.Error(w, http.StatusConflict, "not_confirmed", "Only a confirmed booking can be marked as a no-show.")
	case errors.Is(err, ErrBadStatus):
		httpx.Error(w, http.StatusConflict, "bad_status", "The booking is not in a state that allows that.")
	case errors.Is(err, ErrReason):
		bad(w, "reason_required", "Give a reason for using a room that is not ready.")
	case errors.Is(err, ErrAlreadyCancelled):
		httpx.Error(w, http.StatusConflict, "already_cancelled", "That booking is already cancelled.")
	case errors.Is(err, ErrCannotCancel):
		httpx.Error(w, http.StatusConflict, "cannot_cancel", "A booking that has checked in cannot be cancelled.")
	default:
		httpx.InternalError(w, err)
	}
	return true
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	a, ok := property(w, r)
	if !ok {
		return
	}
	var in CreateInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	var res Reservation
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) (e error) {
		res, e = Create(r.Context(), tx, a.OrgID, a.PropertyID, in, h.Clock())
		return
	})
	if !fail(w, err) {
		httpx.JSON(w, http.StatusCreated, res)
	}
}

func (h *handler) cancel(w http.ResponseWriter, r *http.Request) {
	a, ok := property(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		notFound(w)
		return
	}
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		return Cancel(r.Context(), tx, a.OrgID, a.PropertyID, id, h.Clock())
	})
	if !fail(w, err) {
		w.WriteHeader(http.StatusNoContent)
	}
}

type summary struct {
	ID        uuid.UUID `json:"id"`
	Code      string    `json:"code"`
	Status    string    `json:"status"`
	GuestID   uuid.UUID `json:"guestId"`
	GuestName string    `json:"guestName"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Rooms     int       `json:"rooms"`
	Total     int64     `json:"total"`
	Currency  string    `json:"currency"`
	Source    string    `json:"source"`
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	a, ok := property(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	var from, to *time.Time
	for key, dst := range map[string]**time.Time{"from": &from, "to": &to} {
		if v := q.Get(key); v != "" {
			t, err := time.Parse(day, v)
			if err != nil {
				bad(w, "bad_request", "Dates are written like 2026-12-31.")
				return
			}
			*dst = &t
		}
	}
	limit := 200
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			bad(w, "bad_request", "The limit is not valid.")
			return
		}
		limit = min(n, 500)
	}
	search := "%" + strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(q.Get("q")), `\`, `\\`), "%", `\%`) + "%"
	out := []summary{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(),
			`select r.id, r.code, r.status, g.id, g.name, to_char(min(lower(rr.stay)), 'YYYY-MM-DD'), to_char(max(upper(rr.stay)), 'YYYY-MM-DD'),
			        count(rr.id), r.total, r.currency, r.source_channel
			 from reservation r join guest g on g.id = r.guest_id join reservation_room rr on rr.reservation_id = r.id
			 where r.property_id = $1 and ($2 = '' or r.status = $2)
			   and ($3::date is null or $4::date is null or exists (select 1 from reservation_room x where x.reservation_id = r.id and x.stay && daterange($3, $4, '[)')))
			   and ($5 = '%%' or r.code ilike $5 or g.name ilike $5)
			 group by r.id, g.id order by min(lower(rr.stay)), r.created_at limit $6`,
			a.PropertyID, q.Get("status"), from, to, search, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s summary
			if err := rows.Scan(&s.ID, &s.Code, &s.Status, &s.GuestID, &s.GuestName, &s.From, &s.To, &s.Rooms, &s.Total, &s.Currency, &s.Source); err != nil {
				return err
			}
			out = append(out, s)
		}
		return rows.Err()
	})
	if !fail(w, err) {
		httpx.JSON(w, http.StatusOK, out)
	}
}

type night struct {
	Date  string `json:"date"`
	Price int64  `json:"price"`
}

type roomOut struct {
	ID           uuid.UUID  `json:"id"`
	RoomTypeID   uuid.UUID  `json:"roomTypeId"`
	RoomTypeName string     `json:"roomTypeName"`
	RoomID       *uuid.UUID `json:"roomId"`
	RoomNumber   *string    `json:"roomNumber"`
	RatePlanID   uuid.UUID  `json:"ratePlanId"`
	From         string     `json:"from"`
	To           string     `json:"to"`
	Adults       int        `json:"adults"`
	Children     int        `json:"children"`
	Nights       []night    `json:"nights"`
}

type guestOut struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Email   *string   `json:"email,omitempty"`
	Phone   *string   `json:"phone,omitempty"`
	Country *string   `json:"country,omitempty"`
	Notes   *string   `json:"notes,omitempty"`
	Locale  string    `json:"locale,omitempty"`
}

type detail struct {
	summary
	CreatedAt time.Time `json:"createdAt"`
	Guest     guestOut  `json:"guest"`
	Rooms     []roomOut `json:"rooms"`
}

func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	a, ok := property(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		notFound(w)
		return
	}
	var out detail
	contact := access.Can(a.Role, access.GuestContactView)
	ctx := r.Context()
	err = db.WithOrg(ctx, h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var email, phone, country, notes string
		if err := tx.QueryRow(ctx,
			`select r.id, r.code, r.status, r.total, r.currency, r.source_channel, r.created_at, g.id, g.name, coalesce(g.email, ''), g.phone, g.country, g.notes, g.locale
			 from reservation r join guest g on g.id = r.guest_id where r.id = $1 and r.property_id = $2`, id, a.PropertyID).
			Scan(&out.ID, &out.Code, &out.Status, &out.Total, &out.Currency, &out.Source, &out.CreatedAt, &out.Guest.ID, &out.Guest.Name, &email, &phone, &country, &notes, &out.Guest.Locale); err != nil {
			return ErrNotFound
		}
		out.GuestID, out.GuestName = out.Guest.ID, out.Guest.Name
		if contact {
			out.Guest.Email, out.Guest.Phone, out.Guest.Country, out.Guest.Notes = &email, &phone, &country, &notes
		}
		rows, err := tx.Query(ctx,
			`select rr.id, rr.room_type_id, t.name, rr.room_id, rm.number, rr.rate_plan_id, to_char(lower(rr.stay), 'YYYY-MM-DD'), to_char(upper(rr.stay), 'YYYY-MM-DD'), rr.adults, rr.children
			 from reservation_room rr join room_type t on t.id = rr.room_type_id left join room rm on rm.id = rr.room_id
			 where rr.reservation_id = $1 order by lower(rr.stay), rr.id`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			x := roomOut{Nights: []night{}}
			if err := rows.Scan(&x.ID, &x.RoomTypeID, &x.RoomTypeName, &x.RoomID, &x.RoomNumber, &x.RatePlanID, &x.From, &x.To, &x.Adults, &x.Children); err != nil {
				return err
			}
			out.Rooms = append(out.Rooms, x)
		}
		rows.Close()
		for i := range out.Rooms {
			nr, err := tx.Query(ctx, `select to_char(date, 'YYYY-MM-DD'), price from reservation_night where reservation_room_id = $1 order by date`, out.Rooms[i].ID)
			if err != nil {
				return err
			}
			for nr.Next() {
				var n night
				if err := nr.Scan(&n.Date, &n.Price); err != nil {
					nr.Close()
					return err
				}
				out.Rooms[i].Nights = append(out.Rooms[i].Nights, n)
			}
			nr.Close()
		}
		out.summary.Rooms = len(out.Rooms)
		if len(out.Rooms) > 0 {
			out.From, out.To = out.Rooms[0].From, out.Rooms[len(out.Rooms)-1].To
		}
		return nil
	})
	if !fail(w, err) {
		httpx.JSON(w, http.StatusOK, out)
	}
}

func (h *handler) availability(w http.ResponseWriter, r *http.Request) {
	a, ok := property(w, r)
	if !ok {
		return
	}
	from, e1 := time.Parse(day, r.URL.Query().Get("from"))
	to, e2 := time.Parse(day, r.URL.Query().Get("to"))
	if e1 != nil || e2 != nil || !to.After(from) || to.Sub(from) > 400*24*time.Hour {
		bad(w, "bad_request", "Give a first date and a later last date within 400 days.")
		return
	}
	type row struct {
		RoomTypeID uuid.UUID `json:"roomTypeId"`
		Date       string    `json:"date"`
		Total      int       `json:"total"`
		Sold       int       `json:"sold"`
		Blocked    int       `json:"blocked"`
		Available  int       `json:"available"`
	}
	out := []row{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(),
			`select i.room_type_id, to_char(i.date, 'YYYY-MM-DD'), i.total, i.sold, i.blocked, greatest(i.total - i.sold - i.blocked, 0)
			 from inventory i join room_type t on t.id = i.room_type_id where t.property_id = $1 and i.date >= $2 and i.date < $3 order by i.date, t.name`, a.PropertyID, from, to)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x row
			if err := rows.Scan(&x.RoomTypeID, &x.Date, &x.Total, &x.Sold, &x.Blocked, &x.Available); err != nil {
				return err
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	if !fail(w, err) {
		httpx.JSON(w, http.StatusOK, out)
	}
}

func (h *handler) listGuests(w http.ResponseWriter, r *http.Request) {
	a, ok := property(w, r)
	if !ok {
		return
	}
	contact := access.Can(a.Role, access.GuestContactView)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	search := "%" + strings.ReplaceAll(strings.ReplaceAll(q, `\`, `\\`), "%", `\%`) + "%"
	out := []guestOut{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(),
			`select id, name, coalesce(email, ''), phone, country from guest where ($1 = '%%' or name ilike $1 or ($2 and email ilike $1)) order by name limit 50`, search, contact)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var g guestOut
			var email, phone, country string
			if err := rows.Scan(&g.ID, &g.Name, &email, &phone, &country); err != nil {
				return err
			}
			if contact {
				g.Email, g.Phone, g.Country = &email, &phone, &country
			}
			out = append(out, g)
		}
		return rows.Err()
	})
	if !fail(w, err) {
		httpx.JSON(w, http.StatusOK, out)
	}
}

func (h *handler) getGuest(w http.ResponseWriter, r *http.Request) {
	a, ok := property(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		notFound(w)
		return
	}
	contact := access.Can(a.Role, access.GuestContactView)
	var g guestOut
	var stays []summary
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var email, phone, country, notes string
		if err := tx.QueryRow(r.Context(), `select id, name, coalesce(email, ''), phone, country, notes, locale from guest where id = $1`, id).Scan(&g.ID, &g.Name, &email, &phone, &country, &notes, &g.Locale); err != nil {
			return ErrNotFound
		}
		if contact {
			g.Email, g.Phone, g.Country, g.Notes = &email, &phone, &country, &notes
		}
		rows, err := tx.Query(r.Context(),
			`select r.id, r.code, r.status, to_char(min(lower(rr.stay)), 'YYYY-MM-DD'), to_char(max(upper(rr.stay)), 'YYYY-MM-DD'), r.total, r.currency
			 from reservation r join reservation_room rr on rr.reservation_id = r.id where r.guest_id = $1 and r.property_id = $2 group by r.id order by min(lower(rr.stay)) desc`, id, a.PropertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s summary
			if err := rows.Scan(&s.ID, &s.Code, &s.Status, &s.From, &s.To, &s.Total, &s.Currency); err != nil {
				return err
			}
			stays = append(stays, s)
		}
		return rows.Err()
	})
	if !fail(w, err) {
		httpx.JSON(w, http.StatusOK, map[string]any{"guest": g, "stays": stays})
	}
}

func (h *handler) patchGuest(w http.ResponseWriter, r *http.Request) {
	a, ok := property(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		notFound(w)
		return
	}
	var in struct {
		Name    *string `json:"name"`
		Phone   *string `json:"phone"`
		Country *string `json:"country"`
		Notes   *string `json:"notes"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" || len(n) > 200 {
			bad(w, "bad_request", "A guest needs a name.")
			return
		}
		in.Name = &n
	}
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var name string
		if err := tx.QueryRow(r.Context(),
			`update guest set name = coalesce($2, name), phone = coalesce($3, phone), country = coalesce($4, country), notes = coalesce($5, notes) where id = $1 returning name`,
			id, in.Name, in.Phone, in.Country, in.Notes).Scan(&name); err != nil {
			return ErrNotFound
		}
		return audit.Record(r.Context(), tx, audit.Entry{Action: "guest.changed", TargetType: "guest", TargetID: id, TargetLabel: name})
	})
	if !fail(w, err) {
		w.WriteHeader(http.StatusNoContent)
	}
}
