package notes

import (
	"encoding/json"
	"errors"
	"fmt"
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
	"hotelpms/api/internal/notify"
	"hotelpms/api/internal/session"
)

type handler struct {
	httpx.Deps
	hub *notify.Hub
}

// Mount returns the notes routes; hub carries the live events of an open thread.
func Mount(hub *notify.Hub) httpx.Mount {
	return func(mux *http.ServeMux, d httpx.Deps) {
		h := &handler{d, hub}
		one := func(method string, f http.HandlerFunc, p access.Permission) http.HandlerFunc {
			return httpx.Methods(map[string]http.HandlerFunc{method: httpx.Wrap(access.Require(d, p)(f))})
		}
		view := access.ReservationsView
		mux.HandleFunc("/api/reservations/{id}/thread", one(http.MethodGet, h.thread, view))
		mux.HandleFunc("/api/reservations/{id}/thread/stream", one(http.MethodGet, h.stream, view))
		mux.HandleFunc("/api/reservations/{id}/notes", one(http.MethodPost, h.create, view))
		mux.HandleFunc("/api/notes/unread", one(http.MethodGet, h.unread, view))
		mux.HandleFunc("/api/notes/{id}", httpx.Methods(map[string]http.HandlerFunc{
			http.MethodPatch:  httpx.Wrap(access.Require(d, view)(http.HandlerFunc(h.patch))),
			http.MethodDelete: httpx.Wrap(access.Require(d, view)(http.HandlerFunc(h.remove))),
		}))
		mux.HandleFunc("/api/notes/{id}/done", one(http.MethodPost, h.done, view))
		mux.HandleFunc("/api/colleagues", one(http.MethodGet, h.colleagues, view))
		mux.HandleFunc("/api/rooms/{id}/notes", httpx.Methods(map[string]http.HandlerFunc{
			http.MethodGet:  httpx.Wrap(access.Require(d, access.HousekeepingView)(http.HandlerFunc(h.roomNotes))),
			http.MethodPost: httpx.Wrap(access.Require(d, access.HousekeepingUpdate)(http.HandlerFunc(h.roomReply))),
		}))
	}
}

func bad(w http.ResponseWriter, code, msg string) { httpx.Error(w, http.StatusBadRequest, code, msg) }

func fail(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, ErrNotFound), errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
	case errors.Is(err, ErrBad):
		bad(w, "bad_request", "A note is 1 to 2,000 characters, with up to 20 tags.")
	case errors.Is(err, ErrCannotTag):
		bad(w, "cannot_tag", "You can only tag people who work at this property.")
	case errors.Is(err, ErrGuestName):
		httpx.Error(w, http.StatusConflict, "contains_guest_name", "This note names the guest, and housekeeping will read it. Remove the name, or send it anyway.")
	case errors.Is(err, ErrForbidden):
		httpx.Error(w, http.StatusForbidden, "forbidden", "You do not have permission to do that.")
	case errors.Is(err, ErrEditWindowClosed):
		httpx.Error(w, http.StatusConflict, "edit_window_closed", "A note can be edited for 5 minutes after it is written.")
	case errors.Is(err, ErrAlreadyRemoved):
		httpx.Error(w, http.StatusConflict, "already_removed", "That note is already removed.")
	default:
		httpx.InternalError(w, err)
	}
	return true
}

func scope(w http.ResponseWriter, r *http.Request) (access.Actor, bool) {
	a := access.ActorFrom(r.Context())
	if a.PropertyID == uuid.Nil {
		bad(w, "bad_request", "Choose a property first.")
		return a, false
	}
	return a, true
}

func pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
	}
	return id, err == nil
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	res, ok := pathID(w, r)
	if !ok {
		return
	}
	var in CreateInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	var id uuid.UUID
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) (e error) {
		id, e = Create(r.Context(), tx, a, res, in, h.Clock())
		return
	})
	if !fail(w, err) {
		httpx.JSON(w, http.StatusCreated, map[string]any{"id": id})
	}
}

type mentionOut struct {
	Name   string `json:"name,omitempty"`
	Role   string `json:"role"`
	Done   bool   `json:"done"`
	Person string `json:"person,omitempty"`
}

type item struct {
	Type       string       `json:"type"` // note or event
	ID         *uuid.UUID   `json:"id,omitempty"`
	At         time.Time    `json:"at"`
	Author     string       `json:"author"`
	AuthorRole string       `json:"authorRole,omitempty"`
	Body       *string      `json:"body,omitempty"`
	Removed    bool         `json:"removed,omitempty"`
	RemovedBy  string       `json:"removedBy,omitempty"`
	Pinned     bool         `json:"pinned,omitempty"`
	Edited     bool         `json:"edited,omitempty"`
	Mine       bool         `json:"mine,omitempty"`
	Mentions   []mentionOut `json:"mentions,omitempty"`
	Action     string       `json:"action,omitempty"`
	Label      string       `json:"label,omitempty"`
}

// thread returns the booking's notes and its activity trail in time order. Reading it marks the notes as read.
func (h *handler) thread(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	res, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	out := struct {
		Pinned []item `json:"pinned"`
		Items  []item `json:"items"`
	}{Pinned: []item{}, Items: []item{}}
	err := db.WithOrg(ctx, h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var one int
		if err := tx.QueryRow(ctx, `select 1 from reservation where id = $1 and property_id = $2`, res, a.PropertyID).Scan(&one); err != nil {
			return ErrNotFound
		}
		events, err := tx.Query(ctx,
			`select coalesce(u.name, 'The system'), l.action, l.target_label, l.occurred_at from audit_log l left join app_user u on u.id = l.actor_user_id
			 where l.organization_id = $1 and l.target_type = 'reservation' and l.target_id = $2 order by l.seq`, a.OrgID, res)
		if err != nil {
			return err
		}
		for events.Next() {
			it := item{Type: "event"}
			if err := events.Scan(&it.Author, &it.Action, &it.Label, &it.At); err != nil {
				events.Close()
				return err
			}
			out.Items = append(out.Items, it)
		}
		events.Close()
		if err := events.Err(); err != nil {
			return err
		}
		rows, err := tx.Query(ctx,
			`select n.id, n.created_at, u.name, n.body, n.pinned, n.edited_at is not null, n.removed_at is not null, coalesce(rm.name, ''), n.author_id = $2,
			        coalesce((select m.role from membership m where m.user_id = n.author_id and m.organization_id = n.organization_id and (m.property_id = n.property_id or m.property_id is null) limit 1), '')
			 from note n join app_user u on u.id = n.author_id left join app_user rm on rm.id = n.removed_by where n.reservation_id = $1 order by n.created_at, n.id`, res, a.UserID)
		if err != nil {
			return err
		}
		var noteIDs []uuid.UUID
		for rows.Next() {
			it := item{Type: "note"}
			var id uuid.UUID
			var body string
			if err := rows.Scan(&id, &it.At, &it.Author, &body, &it.Pinned, &it.Edited, &it.Removed, &it.RemovedBy, &it.Mine, &it.AuthorRole); err != nil {
				rows.Close()
				return err
			}
			it.ID = &id
			if !it.Removed { // a removed note's text is kept for owners in the activity trail, not shown here
				it.Body = &body
			}
			noteIDs = append(noteIDs, id)
			out.Items = append(out.Items, it)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for i := range out.Items {
			if out.Items[i].Type != "note" {
				continue
			}
			ms, err := tx.Query(ctx, `select coalesce(u.name, ''), coalesce(m.role, ''), m.done_at is not null from note_mention m left join app_user u on u.id = m.user_id where m.note_id = $1 order by m.id`, *out.Items[i].ID)
			if err != nil {
				return err
			}
			for ms.Next() {
				var m mentionOut
				if err := ms.Scan(&m.Name, &m.Role, &m.Done); err != nil {
					ms.Close()
					return err
				}
				out.Items[i].Mentions = append(out.Items[i].Mentions, m)
			}
			ms.Close()
		}
		for _, id := range noteIDs {
			if _, err := tx.Exec(ctx, `insert into note_read (note_id, user_id, organization_id, read_at) values ($1, $2, $3, $4) on conflict do nothing`, id, a.UserID, a.OrgID, h.Clock()); err != nil {
				return err
			}
		}
		return nil
	})
	if fail(w, err) {
		return
	}
	// Merge by time; a stable sort keeps events before notes written in the same instant.
	sortItems(out.Items)
	for _, it := range out.Items {
		if it.Type == "note" && it.Pinned && !it.Removed {
			out.Pinned = append(out.Pinned, it)
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

func sortItems(items []item) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].At.Before(items[j-1].At); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

func (h *handler) unread(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	out := struct {
		ReservationIDs []uuid.UUID `json:"reservationIds"`
	}{ReservationIDs: []uuid.UUID{}}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(),
			`select distinct n.reservation_id from note n where n.property_id = $1 and n.reservation_id is not null and n.removed_at is null and n.author_id <> $2
			   and not exists (select 1 from note_read x where x.note_id = n.id and x.user_id = $2)`, a.PropertyID, a.UserID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return err
			}
			out.ReservationIDs = append(out.ReservationIDs, id)
		}
		return rows.Err()
	})
	if !fail(w, err) {
		httpx.JSON(w, http.StatusOK, out)
	}
}

func (h *handler) patch(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Body   *string `json:"body"`
		Pinned *bool   `json:"pinned"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	ctx := r.Context()
	err := db.WithOrg(ctx, h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var author uuid.UUID
		var created time.Time
		var reservation *uuid.UUID
		var removed bool
		if err := tx.QueryRow(ctx, `select author_id, created_at, reservation_id, removed_at is not null from note where id = $1 and property_id = $2 for update`, id, a.PropertyID).Scan(&author, &created, &reservation, &removed); err != nil || removed {
			return ErrNotFound
		}
		mine, manager := author == a.UserID, access.Can(a.Role, access.UsersManage)
		if in.Body != nil {
			body := strings.TrimSpace(*in.Body)
			if !mine {
				return ErrForbidden
			}
			if body == "" || len([]rune(body)) > maxBody {
				return ErrBad
			}
			if h.Clock().Sub(created) > editWindow {
				return ErrEditWindowClosed
			}
			if _, err := tx.Exec(ctx, `update note set body = $2, edited_at = $3 where id = $1`, id, body, h.Clock()); err != nil {
				return err
			}
		}
		if in.Pinned != nil {
			if !mine && !manager {
				return ErrForbidden
			}
			if _, err := tx.Exec(ctx, `update note set pinned = $2 where id = $1`, id, *in.Pinned); err != nil {
				return err
			}
		}
		if reservation != nil {
			if _, err := tx.Exec(ctx, `select pg_notify('notes', $1)`, reservation.String()); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{Action: "note.changed", TargetType: "note", TargetID: id, After: map[string]any{"edited": in.Body != nil, "pinned": in.Pinned}})
	})
	if !fail(w, err) {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *handler) remove(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	err := db.WithOrg(ctx, h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var author uuid.UUID
		var body string
		var reservation *uuid.UUID
		var removed bool
		if err := tx.QueryRow(ctx, `select author_id, body, reservation_id, removed_at is not null from note where id = $1 and property_id = $2 for update`, id, a.PropertyID).Scan(&author, &body, &reservation, &removed); err != nil {
			return ErrNotFound
		}
		if author != a.UserID && !access.Can(a.Role, access.UsersManage) {
			return ErrForbidden
		}
		if removed {
			return ErrAlreadyRemoved
		}
		if _, err := tx.Exec(ctx, `update note set removed_at = $2, removed_by = $3 where id = $1`, id, h.Clock(), a.UserID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `select pg_notify('notes', $1)`, reservation.String()); err != nil {
			return err
		}
		// The text stays readable in the activity trail, which only owners and managers can open.
		return audit.Record(ctx, tx, audit.Entry{Action: "note.removed", TargetType: "note", TargetID: id, Before: map[string]string{"body": body}})
	})
	if !fail(w, err) {
		w.WriteHeader(http.StatusNoContent)
	}
}

// done marks the tags aimed at the person (by name or by their role) as answered.
func (h *handler) done(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(r.Context(),
			`select count(*) from note_mention m join note n on n.id = m.note_id where m.note_id = $1 and n.property_id = $2 and (m.user_id = $3 or m.role = $4)`, id, a.PropertyID, a.UserID, string(a.Role)).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		_, err := tx.Exec(r.Context(), `update note_mention set done_at = $4 where note_id = $1 and done_at is null and (user_id = $2 or role = $3)`, id, a.UserID, string(a.Role), h.Clock())
		return err
	})
	if !fail(w, err) {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *handler) colleagues(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	q := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`).Replace(strings.TrimSpace(r.URL.Query().Get("q"))) + "%"
	type row struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
		Role string    `json:"role"`
	}
	out := []row{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(),
			`select distinct on (u.id) u.id, u.name, m.role from membership m join app_user u on u.id = m.user_id
			 where m.organization_id = $1 and m.status = 'active' and (m.property_id = $2 or m.property_id is null) and u.name ilike $3
			 order by u.id, m.property_id nulls last`, a.OrgID, a.PropertyID, q)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x row
			if err := rows.Scan(&x.ID, &x.Name, &x.Role); err != nil {
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

// roomNotes is what housekeeping sees on a room: notes that tagged housekeeping, and replies. The text, the room
// number and the author. There is no booking, guest or price in it.
func (h *handler) roomNotes(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	room, ok := pathID(w, r)
	if !ok {
		return
	}
	type row struct {
		ID         uuid.UUID `json:"id"`
		RoomNumber string    `json:"roomNumber"`
		Author     string    `json:"author"`
		Body       string    `json:"body"`
		CreatedAt  time.Time `json:"createdAt"`
		Mine       bool      `json:"mine"`
	}
	out := []row{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(),
			`select n.id, rm.number, u.name, n.body, n.created_at, n.author_id = $3 from note n join room rm on rm.id = n.room_id join app_user u on u.id = n.author_id
			 where n.room_id = $1 and rm.property_id = $2 and n.for_housekeeping and n.removed_at is null order by n.created_at`, room, a.PropertyID, a.UserID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x row
			if err := rows.Scan(&x.ID, &x.RoomNumber, &x.Author, &x.Body, &x.CreatedAt, &x.Mine); err != nil {
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

// roomReply lets a housekeeper answer a note on a room. The reply joins the booking's thread.
func (h *handler) roomReply(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	room, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Body    string `json:"body"`
		ReplyTo string `json:"replyTo"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	parent, perr := uuid.Parse(in.ReplyTo)
	body := strings.TrimSpace(in.Body)
	if body == "" || len([]rune(body)) > maxBody {
		bad(w, "bad_request", "A reply is 1 to 2,000 characters.")
		return
	}
	id := ids.New()
	ctx := r.Context()
	err := db.WithOrg(ctx, h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var reservation uuid.UUID
		if perr != nil {
			return ErrNotFound
		}
		if err := tx.QueryRow(ctx, `select reservation_id from note where id = $1 and room_id = $2 and property_id = $3 and for_housekeeping and removed_at is null and reservation_id is not null`, parent, room, a.PropertyID).Scan(&reservation); err != nil {
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx,
			`insert into note (id, organization_id, property_id, reservation_id, room_id, parent_id, author_id, body, for_housekeeping, created_at) values ($1, $2, $3, $4, $5, $6, $7, $8, true, $9)`,
			id, a.OrgID, a.PropertyID, reservation, room, parent, a.UserID, body, h.Clock()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `select pg_notify('notes', $1)`, reservation.String()); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Action: "note.created", TargetType: "note", TargetID: id, After: map[string]any{"reply": true}})
	})
	if !fail(w, err) {
		httpx.JSON(w, http.StatusCreated, map[string]any{"id": id})
	}
}

// stream tells an open booking screen to reload its thread the moment someone writes, edits or removes a note.
func (h *handler) stream(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	res, ok := pathID(w, r)
	if !ok {
		return
	}
	flusher, canFlush := w.(http.Flusher)
	s := httpx.SessionFrom(r.Context())
	var one int
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `select 1 from reservation where id = $1 and property_id = $2`, res, a.PropertyID).Scan(&one)
	})
	if !canFlush || err != nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
		return
	}
	wake, stop := h.hub.SubscribeTopic("notes:" + res.String())
	defer stop()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	beat := time.NewTicker(25 * time.Second)
	defer beat.Stop()
	alive := func() bool {
		err := db.WithoutOrg(r.Context(), h.Pool, func(tx pgx.Tx) error {
			_, e := session.Lookup(r.Context(), tx, s.ID, h.Clock())
			return e
		})
		return err == nil
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-beat.C:
			if !alive() {
				return
			}
			fmt.Fprint(w, ": keep-alive\n\n")
		case <-wake:
			if !alive() {
				return
			}
			body, _ := json.Marshal(map[string]string{"reservationId": res.String()})
			fmt.Fprintf(w, "event: note\ndata: %s\n\n", body)
		}
		flusher.Flush()
	}
}

