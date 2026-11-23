package housekeeping

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/ids"
)

type handler struct{ httpx.Deps }

func Mount(mux *http.ServeMux, d httpx.Deps) {
	h := &handler{d}
	view, update := access.HousekeepingView, access.HousekeepingUpdate
	route := func(m map[string]http.HandlerFunc, p map[string]access.Permission) http.HandlerFunc {
		out := map[string]http.HandlerFunc{}
		for method, f := range m {
			out[method] = httpx.Wrap(access.Require(d, p[method])(f))
		}
		return httpx.Methods(out)
	}
	mux.HandleFunc("/api/housekeeping/tasks", route(map[string]http.HandlerFunc{http.MethodGet: h.listTasks}, map[string]access.Permission{http.MethodGet: view}))
	mux.HandleFunc("/api/housekeeping/tasks/{id}", route(map[string]http.HandlerFunc{http.MethodPatch: h.patchTask}, map[string]access.Permission{http.MethodPatch: update}))
	mux.HandleFunc("/api/rooms/{id}/status", route(map[string]http.HandlerFunc{http.MethodPatch: h.setRoomStatus}, map[string]access.Permission{http.MethodPatch: update}))
	mux.HandleFunc("/api/maintenance-tickets", route(
		map[string]http.HandlerFunc{http.MethodGet: h.listTickets, http.MethodPost: h.createTicket},
		map[string]access.Permission{http.MethodGet: view, http.MethodPost: update}))
	mux.HandleFunc("/api/maintenance-tickets/{id}", route(map[string]http.HandlerFunc{http.MethodPatch: h.patchTicket}, map[string]access.Permission{http.MethodPatch: update}))
}

func bad(w http.ResponseWriter, msg string) {
	httpx.Error(w, http.StatusBadRequest, "bad_request", msg)
}

func scope(w http.ResponseWriter, r *http.Request) (access.Actor, bool) {
	a := access.ActorFrom(r.Context())
	if a.PropertyID == uuid.Nil {
		bad(w, "Choose a property first.")
		return a, false
	}
	return a, true
}

func fail(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, ErrNotFound), errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
	case errors.Is(err, ErrBad):
		bad(w, "Check the status, the dates and the text.")
	case errors.Is(err, ErrAlreadyClosed):
		httpx.Error(w, http.StatusConflict, "already_closed", "That ticket is already closed.")
	default:
		httpx.InternalError(w, err)
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
	}
	return id, err == nil
}

const day = "2006-01-02"

func (h *handler) listTasks(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	date, err := time.Parse(day, r.URL.Query().Get("date"))
	if err != nil {
		bad(w, "Give the date like 2026-12-31.")
		return
	}
	type row struct {
		ID             uuid.UUID  `json:"id"`
		RoomID         uuid.UUID  `json:"roomId"`
		RoomNumber     string     `json:"roomNumber"`
		Floor          int        `json:"floor"`
		RoomType       string     `json:"roomType"`
		Kind           string     `json:"kind"`
		Status         string     `json:"status"`
		RoomStatus     string     `json:"roomStatus"`
		AssignedTo     *uuid.UUID `json:"assignedTo"`
		ArrivalToday   bool       `json:"arrivalToday"`
		DepartureToday bool       `json:"departureToday"`
	}
	out := []row{}
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		// Only the room, its cleaning and whether someone arrives or leaves today. Never who.
		rows, err := tx.Query(r.Context(),
			`select t.id, rm.id, rm.number, rm.floor, rt.name, t.kind, t.status, rm.status, t.assigned_to,
			        exists (select 1 from reservation_room x where x.room_id = rm.id and x.active and lower(x.stay) = $2),
			        exists (select 1 from reservation_room x where x.room_id = rm.id and x.active and upper(x.stay) = $2)
			 from housekeeping_task t join room rm on rm.id = t.room_id join room_type rt on rt.id = rm.room_type_id
			 where t.property_id = $1 and t.date = $2 order by t.status = 'done', rm.floor, rm.number`, a.PropertyID, date)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x row
			if err := rows.Scan(&x.ID, &x.RoomID, &x.RoomNumber, &x.Floor, &x.RoomType, &x.Kind, &x.Status, &x.RoomStatus, &x.AssignedTo, &x.ArrivalToday, &x.DepartureToday); err != nil {
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

func (h *handler) patchTask(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Status     *string `json:"status"`
		AssignedTo *string `json:"assignedTo"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	if in.Status != nil && *in.Status != "todo" && *in.Status != "in_progress" && *in.Status != "done" {
		bad(w, "Status is todo, in_progress or done.")
		return
	}
	var assignee *uuid.UUID
	if in.AssignedTo != nil {
		v, err := uuid.Parse(*in.AssignedTo)
		if err != nil {
			bad(w, "Choose a person.")
			return
		}
		assignee = &v
	}
	ctx := r.Context()
	err := db.WithOrg(ctx, h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var roomID uuid.UUID
		var before string
		if err := tx.QueryRow(ctx, `select room_id, status from housekeeping_task where id = $1 and property_id = $2 for update`, id, a.PropertyID).Scan(&roomID, &before); err != nil {
			return ErrNotFound
		}
		if in.Status != nil {
			var done *time.Time
			if *in.Status == "done" {
				now := h.Clock()
				done = &now
			}
			if _, err := tx.Exec(ctx, `update housekeeping_task set status = $2, done_at = $3 where id = $1`, id, *in.Status, done); err != nil {
				return err
			}
			if *in.Status == "done" && before != "done" {
				if _, err := tx.Exec(ctx, `update room set status = 'clean' where id = $1 and status = 'dirty'`, roomID); err != nil {
					return err
				}
			}
		}
		if assignee != nil {
			if _, err := tx.Exec(ctx, `update housekeeping_task set assigned_to = $2 where id = $1`, id, *assignee); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{Action: "housekeeping.task_changed", TargetType: "room", TargetID: roomID, Before: map[string]string{"status": before}, After: in})
	})
	if !fail(w, err) {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *handler) setRoomStatus(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct{ Status string }
	if !httpx.Decode(w, r, &in) {
		return
	}
	if in.Status != "clean" && in.Status != "dirty" && in.Status != "inspect" {
		bad(w, "Status is clean, dirty or inspect. A room goes out of order through a maintenance ticket.")
		return
	}
	ctx := r.Context()
	err := db.WithOrg(ctx, h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var before, number string
		if err := tx.QueryRow(ctx, `select status, number from room where id = $1 and property_id = $2 for update`, id, a.PropertyID).Scan(&before, &number); err != nil {
			return ErrNotFound
		}
		if before == "out_of_order" {
			return ErrBad
		}
		if _, err := tx.Exec(ctx, `update room set status = $2 where id = $1`, id, in.Status); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Action: "room.status_changed", TargetType: "room", TargetID: id, TargetLabel: number,
			Before: map[string]string{"status": before}, After: map[string]string{"status": in.Status}})
	})
	if !fail(w, err) {
		w.WriteHeader(http.StatusNoContent)
	}
}

type ticketOut struct {
	ID          uuid.UUID `json:"id"`
	RoomID      uuid.UUID `json:"roomId"`
	RoomNumber  string    `json:"roomNumber"`
	Floor       int       `json:"floor"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	BlocksFrom  *string   `json:"blocksFrom"`
	BlocksTo    *string   `json:"blocksTo"`
	Resolution  string    `json:"resolution"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (h *handler) listTickets(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	out := []ticketOut{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(),
			`select t.id, rm.id, rm.number, rm.floor, t.description, t.status, to_char(t.blocks_from, 'YYYY-MM-DD'), to_char(t.blocks_to, 'YYYY-MM-DD'), t.resolution, t.created_at
			 from maintenance_ticket t join room rm on rm.id = t.room_id where t.property_id = $1 order by t.status = 'done', t.created_at desc`, a.PropertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x ticketOut
			if err := rows.Scan(&x.ID, &x.RoomID, &x.RoomNumber, &x.Floor, &x.Description, &x.Status, &x.BlocksFrom, &x.BlocksTo, &x.Resolution, &x.CreatedAt); err != nil {
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

// block takes the room out of sale for the nights it still can: a night already full of bookings cannot lose a room.
// Closing the ticket gives back exactly the nights recorded here.
func block(ctx context.Context, tx pgx.Tx, orgID, ticketID, roomID uuid.UUID, from, to time.Time) error {
	_, err := tx.Exec(ctx,
		`with typ as (select room_type_id as id from room where id = $3),
		      locked as (select i.date from inventory i, typ where i.room_type_id = typ.id and i.date >= $4 and i.date < $5 and i.sold + i.blocked < i.total order by i.date for update),
		      bumped as (update inventory i set blocked = blocked + 1 from locked l, typ where i.room_type_id = typ.id and i.date = l.date returning i.date)
		 insert into ticket_block (ticket_id, date, organization_id) select $1, date, $2 from bumped`, ticketID, orgID, roomID, from, to)
	return err
}

func (h *handler) createTicket(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	var in struct {
		RoomID      string  `json:"roomId"`
		Description string  `json:"description"`
		BlocksFrom  *string `json:"blocksFrom"`
		BlocksTo    *string `json:"blocksTo"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Description = strings.TrimSpace(in.Description)
	roomID, rerr := uuid.Parse(in.RoomID)
	var from, to *time.Time
	if (in.BlocksFrom == nil) != (in.BlocksTo == nil) {
		bad(w, "Give both dates of the block, or neither.")
		return
	}
	if in.BlocksFrom != nil {
		f, e1 := time.Parse(day, *in.BlocksFrom)
		t, e2 := time.Parse(day, *in.BlocksTo)
		if e1 != nil || e2 != nil || !t.After(f) || t.Sub(f) > 400*24*time.Hour {
			bad(w, "The block needs a first date and a later last date within 400 days.")
			return
		}
		from, to = &f, &t
	}
	if in.Description == "" || len(in.Description) > 1000 || rerr != nil {
		bad(w, "Describe the problem and choose a room.")
		return
	}
	id := ids.New()
	clashes := []uuid.UUID{}
	ctx := r.Context()
	err := db.WithOrg(ctx, h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var number string
		if err := tx.QueryRow(ctx, `select number from room where id = $1 and property_id = $2`, roomID, a.PropertyID).Scan(&number); err != nil {
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx, `insert into maintenance_ticket (id, organization_id, property_id, room_id, description, blocks_from, blocks_to, created_by, created_at) values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			id, a.OrgID, a.PropertyID, roomID, in.Description, from, to, a.UserID, h.Clock()); err != nil {
			return err
		}
		if from != nil {
			if err := block(ctx, tx, a.OrgID, id, roomID, *from, *to); err != nil {
				return err
			}
			rows, err := tx.Query(ctx, `select distinct r.id from reservation_room rr join reservation r on r.id = rr.reservation_id where rr.room_id = $1 and rr.active and rr.stay && daterange($2, $3, '[)')`, roomID, *from, *to)
			if err != nil {
				return err
			}
			for rows.Next() {
				var rid uuid.UUID
				if err := rows.Scan(&rid); err != nil {
					rows.Close()
					return err
				}
				clashes = append(clashes, rid)
			}
			rows.Close()
		}
		return audit.Record(ctx, tx, audit.Entry{Action: "maintenance.opened", TargetType: "room", TargetID: roomID, TargetLabel: number, After: map[string]any{"description": in.Description, "blocks": from != nil}})
	})
	if !fail(w, err) {
		httpx.JSON(w, http.StatusCreated, map[string]any{"id": id, "clashes": clashes})
	}
}

func (h *handler) patchTicket(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Status     string `json:"status"`
		Resolution string `json:"resolution"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	if in.Status != "open" && in.Status != "in_progress" && in.Status != "done" {
		bad(w, "Status is open, in_progress or done.")
		return
	}
	ctx := r.Context()
	err := db.WithOrg(ctx, h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var before string
		var roomID uuid.UUID
		if err := tx.QueryRow(ctx, `select status, room_id from maintenance_ticket where id = $1 and property_id = $2 for update`, id, a.PropertyID).Scan(&before, &roomID); err != nil {
			return ErrNotFound
		}
		if before == "done" {
			return ErrAlreadyClosed
		}
		var closed *time.Time
		if in.Status == "done" {
			now := h.Clock()
			closed = &now
			if _, err := tx.Exec(ctx,
				`update inventory i set blocked = blocked - 1 from ticket_block b, room rm
				 where b.ticket_id = $1 and rm.id = $2 and i.room_type_id = rm.room_type_id and i.date = b.date`, id, roomID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `update maintenance_ticket set status = $2, resolution = $3, closed_at = $4 where id = $1`, id, in.Status, strings.TrimSpace(in.Resolution), closed); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Action: "maintenance." + in.Status, TargetType: "room", TargetID: roomID, Before: map[string]string{"status": before}, After: map[string]string{"status": in.Status}})
	})
	if !fail(w, err) {
		w.WriteHeader(http.StatusNoContent)
	}
}
