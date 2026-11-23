package booking

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
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/rooms"
)

var (
	ErrWrongType        = errors.New("booking: the room is not of the booked type")
	ErrRoomNotReady     = errors.New("booking: the room is not ready")
	ErrRoomTaken        = errors.New("booking: the room holds another stay on those nights")
	ErrNoRoom           = errors.New("booking: a room has not been assigned")
	ErrTooEarly         = errors.New("booking: the stay has not started")
	ErrAlreadyCheckedIn = errors.New("booking: already checked in")
	ErrNotCheckedIn     = errors.New("booking: not checked in")
	ErrNotConfirmed     = errors.New("booking: not a confirmed booking")
	ErrReason           = errors.New("booking: a reason is needed")
	ErrBadStatus        = errors.New("booking: the booking is in the wrong state for that")
)

func lock(ctx context.Context, tx pgx.Tx, propertyID, id uuid.UUID) (status, code string, err error) {
	if err = tx.QueryRow(ctx, `select status, code from reservation where id = $1 and property_id = $2 for update`, id, propertyID).Scan(&status, &code); err != nil {
		return "", "", ErrNotFound
	}
	return
}

func setStatus(ctx context.Context, tx pgx.Tx, id uuid.UUID, code, action, from, to string) error {
	if _, err := tx.Exec(ctx, `update reservation set status = $2 where id = $1`, id, to); err != nil {
		return err
	}
	return audit.Record(ctx, tx, audit.Entry{Action: action, TargetType: "reservation", TargetID: id, TargetLabel: code,
		Before: map[string]string{"status": from}, After: map[string]string{"status": to}})
}

// Assign puts a room on a reservation. A room that is dirty or being inspected needs force and a reason; a room
// that is out of order is never assigned. The exclusion constraint refuses a room that holds an overlapping stay.
func Assign(ctx context.Context, tx pgx.Tx, propertyID, id, roomID uuid.UUID, reservationRoom *uuid.UUID, force bool, reason string) error {
	status, code, err := lock(ctx, tx, propertyID, id)
	if err != nil {
		return err
	}
	if status != "confirmed" && status != "pending" && status != "checked_in" {
		return ErrBadStatus
	}
	var rrID, typeID uuid.UUID
	q := `select id, room_type_id from reservation_room where reservation_id = $1 and active and ($2::uuid is null or id = $2) order by room_id is not null, lower(stay), id limit 1`
	if err := tx.QueryRow(ctx, q, id, reservationRoom).Scan(&rrID, &typeID); err != nil {
		return ErrNotFound
	}
	var roomType uuid.UUID
	var roomStatus, number string
	if err := tx.QueryRow(ctx, `select room_type_id, status, number from room where id = $1 and property_id = $2`, roomID, propertyID).Scan(&roomType, &roomStatus, &number); err != nil {
		return ErrNotFound
	}
	if roomType != typeID {
		return ErrWrongType
	}
	switch {
	case roomStatus == "out_of_order":
		return ErrRoomNotReady
	case roomStatus != "clean" && roomStatus != "inspect" && !force:
		return ErrRoomNotReady
	case roomStatus != "clean" && roomStatus != "inspect" && strings.TrimSpace(reason) == "":
		return ErrReason
	}
	if _, err := tx.Exec(ctx, `update reservation_room set room_id = $2 where id = $1`, rrID, roomID); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23P01" {
			return ErrRoomTaken
		}
		return err
	}
	return audit.Record(ctx, tx, audit.Entry{Action: "reservation.room_assigned", TargetType: "reservation", TargetID: id, TargetLabel: code,
		After: map[string]any{"room": number, "forced": force && roomStatus != "clean" && roomStatus != "inspect"}, Reason: strings.TrimSpace(reason)})
}

// CheckIn marks the guest as arrived. Every room of the booking needs a room number, and it cannot be before the arrival day.
func CheckIn(ctx context.Context, tx pgx.Tx, orgID, propertyID, id uuid.UUID, now time.Time) error {
	status, code, err := lock(ctx, tx, propertyID, id)
	if err != nil {
		return err
	}
	switch status {
	case "checked_in":
		return ErrAlreadyCheckedIn
	case "confirmed":
	default:
		return ErrBadStatus
	}
	var unassigned int
	var first time.Time
	if err := tx.QueryRow(ctx, `select count(*) filter (where room_id is null), min(lower(stay)) from reservation_room where reservation_id = $1 and active`, id).Scan(&unassigned, &first); err != nil {
		return err
	}
	if unassigned > 0 {
		return ErrNoRoom
	}
	today, _, err := HotelToday(ctx, tx, orgID, propertyID, now)
	if err != nil {
		return err
	}
	if today.Before(first) {
		return ErrTooEarly
	}
	return setStatus(ctx, tx, id, code, "reservation.checked_in", status, "checked_in")
}

// release gives the nights of the reservation's active rooms from date on back to inventory.
func release(ctx context.Context, tx pgx.Tx, id uuid.UUID, from *time.Time) error {
	_, err := tx.Exec(ctx,
		`with active as (select room_type_id, stay from reservation_room where reservation_id = $1 and active)
		 update inventory i set sold = sold - 1 from active a
		 where i.room_type_id = a.room_type_id and i.date >= greatest(lower(a.stay), coalesce($2::date, lower(a.stay))) and i.date < upper(a.stay)`, id, from)
	return err
}

// CheckOut ends the stay and marks the rooms dirty. A guest who leaves before the booked departure frees
// the rest of the nights, and pays only for the nights used.
func CheckOut(ctx context.Context, tx pgx.Tx, orgID, propertyID, id uuid.UUID, now time.Time) error {
	status, code, err := lock(ctx, tx, propertyID, id)
	if err != nil {
		return err
	}
	if status != "checked_in" {
		return ErrNotCheckedIn
	}
	today, _, err := HotelToday(ctx, tx, orgID, propertyID, now)
	if err != nil {
		return err
	}
	var early int
	if err := tx.QueryRow(ctx, `select count(*) from reservation_room where reservation_id = $1 and active and upper(stay) > $2`, id, today).Scan(&early); err != nil {
		return err
	}
	if early > 0 {
		if err := release(ctx, tx, id, &today); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`delete from reservation_night n using reservation_room rr
			 where n.reservation_room_id = rr.id and rr.reservation_id = $1 and rr.active and n.date >= $2`, id, today); err != nil {
			return err
		}
		// A stay that ends today keeps its used nights; one that would be empty is closed.
		if _, err := tx.Exec(ctx, `update reservation_room set stay = daterange(lower(stay), $2, '[)') where reservation_id = $1 and active and upper(stay) > $2 and lower(stay) < $2`, id, today); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `update reservation_room set active = false where reservation_id = $1 and active and upper(stay) > $2`, id, today); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `update reservation r set total = coalesce((select sum(n.price) from reservation_night n join reservation_room rr on rr.id = n.reservation_room_id where rr.reservation_id = r.id), 0) where r.id = $1`, id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `update room set status = 'dirty' where id in (select room_id from reservation_room where reservation_id = $1 and room_id is not null)`, id); err != nil {
		return err
	}
	return setStatus(ctx, tx, id, code, "reservation.checked_out", status, "checked_out")
}

// NoShow releases the rooms of a confirmed booking whose guest did not arrive. It cannot be done before the arrival day.
func NoShow(ctx context.Context, tx pgx.Tx, orgID, propertyID, id uuid.UUID, now time.Time) error {
	status, code, err := lock(ctx, tx, propertyID, id)
	if err != nil {
		return err
	}
	if status != "confirmed" {
		return ErrNotConfirmed
	}
	today, _, err := HotelToday(ctx, tx, orgID, propertyID, now)
	if err != nil {
		return err
	}
	var first time.Time
	if err := tx.QueryRow(ctx, `select min(lower(stay)) from reservation_room where reservation_id = $1 and active`, id).Scan(&first); err != nil {
		return err
	}
	if today.Before(first) {
		return ErrTooEarly
	}
	if err := release(ctx, tx, id, nil); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `update reservation_room set active = false where reservation_id = $1`, id); err != nil {
		return err
	}
	return setStatus(ctx, tx, id, code, "reservation.no_show", status, "no_show")
}

// Move puts one room of a booking into another room type for the same nights, at the other rate plan's prices.
// The new type must have the nights free; the old nights are given back. The room number is cleared.
func Move(ctx context.Context, tx pgx.Tx, orgID, propertyID, id, roomTypeID, ratePlanID uuid.UUID, reservationRoom *uuid.UUID, now time.Time) error {
	status, code, err := lock(ctx, tx, propertyID, id)
	if err != nil {
		return err
	}
	if status != "confirmed" && status != "pending" {
		return ErrBadStatus
	}
	var rrID, oldType uuid.UUID
	var from, to time.Time
	var adults, children int
	if err := tx.QueryRow(ctx,
		`select id, room_type_id, lower(stay), upper(stay), adults, children from reservation_room
		 where reservation_id = $1 and active and ($2::uuid is null or id = $2) order by lower(stay), id limit 1`, id, reservationRoom).Scan(&rrID, &oldType, &from, &to, &adults, &children); err != nil {
		return ErrNotFound
	}
	var one int
	if err := tx.QueryRow(ctx, `select 1 from rate_plan where id = $1 and room_type_id = $2 and property_id = $3`, ratePlanID, roomTypeID, propertyID).Scan(&one); err != nil {
		return ErrNotFound
	}
	nights, total, err := rooms.Price(ctx, tx, ratePlanID, from, to, adults, children)
	if err != nil {
		return err
	}
	if err := checkRules(ctx, tx, ratePlanID, from, to); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx,
		`with locked as (select date from inventory where room_type_id = $1 and date >= $2 and date < $3 order by date for update)
		 update inventory i set sold = sold + 1 from locked l where i.room_type_id = $1 and i.date = l.date and i.sold + i.blocked < i.total`, roomTypeID, from, to)
	if err != nil {
		return err
	}
	if int(tag.RowsAffected()) != len(nights) {
		return ErrSoldOut
	}
	if _, err := tx.Exec(ctx, `update inventory set sold = sold - 1 where room_type_id = $1 and date >= $2 and date < $3`, oldType, from, to); err != nil {
		return err
	}
	var oldTotal int64
	if err := tx.QueryRow(ctx, `select coalesce(sum(price), 0) from reservation_night where reservation_room_id = $1`, rrID).Scan(&oldTotal); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from reservation_night where reservation_room_id = $1`, rrID); err != nil {
		return err
	}
	for _, n := range nights {
		if _, err := tx.Exec(ctx, `insert into reservation_night (reservation_room_id, date, organization_id, price) values ($1, $2, $3, $4)`, rrID, n.Date, orgID, n.Price); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `update reservation_room set room_type_id = $2, rate_plan_id = $3, room_id = null where id = $1`, rrID, roomTypeID, ratePlanID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `update reservation set total = total - $2 + $3 where id = $1`, id, oldTotal, total); err != nil {
		return err
	}
	return audit.Record(ctx, tx, audit.Entry{Action: "reservation.moved", TargetType: "reservation", TargetID: id, TargetLabel: code,
		Before: map[string]any{"roomTypeId": oldType, "total": oldTotal}, After: map[string]any{"roomTypeId": roomTypeID, "total": total}})
}

// mountStay adds the stay routes. Check-in and check-out need the check-in permission; the rest need edit rights.
func mountStay(mux *http.ServeMux, h *handler, d httpx.Deps) {
	post := func(p access.Permission, f http.HandlerFunc) http.HandlerFunc {
		return httpx.Methods(map[string]http.HandlerFunc{http.MethodPost: httpx.Wrap(access.Require(d, p)(f))})
	}
	mux.HandleFunc("/api/reservations/{id}/assign", post(access.ReservationsEdit, h.assign))
	mux.HandleFunc("/api/reservations/{id}/check-in", post(access.CheckIn, h.simple(CheckIn)))
	mux.HandleFunc("/api/reservations/{id}/check-out", post(access.CheckIn, h.simple(CheckOut)))
	mux.HandleFunc("/api/reservations/{id}/no-show", post(access.ReservationsEdit, h.simple(NoShow)))
	mux.HandleFunc("/api/reservations/{id}/move", post(access.ReservationsEdit, h.move))
}

type stayFunc func(ctx context.Context, tx pgx.Tx, orgID, propertyID, id uuid.UUID, now time.Time) error

func (h *handler) simple(f stayFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, ok := property(w, r)
		if !ok {
			return
		}
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			notFound(w)
			return
		}
		err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error { return f(r.Context(), tx, a.OrgID, a.PropertyID, id, h.Clock()) })
		if !fail(w, err) {
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

func (h *handler) assign(w http.ResponseWriter, r *http.Request) {
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
		RoomID          string  `json:"roomId"`
		ReservationRoom *string `json:"reservationRoomId"`
		Force           bool    `json:"force"`
		Reason          string  `json:"reason"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	roomID, err := uuid.Parse(in.RoomID)
	if err != nil {
		bad(w, "bad_request", "Choose a room.")
		return
	}
	var rr *uuid.UUID
	if in.ReservationRoom != nil {
		v, err := uuid.Parse(*in.ReservationRoom)
		if err != nil {
			bad(w, "bad_request", "That room of the booking is not valid.")
			return
		}
		rr = &v
	}
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		return Assign(r.Context(), tx, a.PropertyID, id, roomID, rr, in.Force, in.Reason)
	})
	if !fail(w, err) {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *handler) move(w http.ResponseWriter, r *http.Request) {
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
		RoomTypeID      uuid.UUID  `json:"roomTypeId"`
		RatePlanID      uuid.UUID  `json:"ratePlanId"`
		ReservationRoom *uuid.UUID `json:"reservationRoomId"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		return Move(r.Context(), tx, a.OrgID, a.PropertyID, id, in.RoomTypeID, in.RatePlanID, in.ReservationRoom, h.Clock())
	})
	if !fail(w, err) {
		w.WriteHeader(http.StatusNoContent)
	}
}
