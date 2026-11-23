package guestbooking

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/ids"
	"hotelpms/api/internal/mail"
	"hotelpms/api/internal/notify"
)

type messageOut struct {
	Sender    string    `json:"sender"` // guest or staff
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

func loadMessages(ctx context.Context, tx pgx.Tx, res uuid.UUID) ([]messageOut, error) {
	rows, err := tx.Query(ctx, `select sender, body, created_at from guest_message where reservation_id = $1 order by created_at, id`, res)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []messageOut{}
	for rows.Next() {
		var m messageOut
		if err := rows.Scan(&m.Sender, &m.Body, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// readMessage reads and checks the text of a new message. It answers the sender and returns false when the text is not fit.
func readMessage(w http.ResponseWriter, r *http.Request) (string, bool) {
	var in struct {
		Body string `json:"body"`
	}
	if !readJSON(w, r, &in) {
		return "", false
	}
	in.Body = strings.TrimSpace(in.Body)
	if in.Body == "" || utf8.RuneCountInString(in.Body) > 2000 {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Write a message of up to 2000 characters.")
		return "", false
	}
	return in.Body, true
}

func (h *handler) guestMessages(w http.ResponseWriter, r *http.Request, tx pgx.Tx, a booked) error {
	out, err := loadMessages(r.Context(), tx, a.res)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

func (h *handler) guestSends(w http.ResponseWriter, r *http.Request, tx pgx.Tx, a booked) error {
	body, ok := readMessage(w, r)
	if !ok {
		return errRollbackPreview
	}
	now := h.Clock()
	if _, err := tx.Exec(r.Context(), `insert into guest_message (id, organization_id, property_id, reservation_id, sender, body, created_at) values ($1, $2, $3, $4, 'guest', $5, $6)`,
		ids.New(), a.org, a.prop, a.res, body, now); err != nil {
		return err
	}
	// One open notification for the front desk per booking, which counts the new messages.
	if err := notify.Send(r.Context(), tx, notify.Input{OrgID: a.org, PropertyID: a.prop, ToRole: access.FrontDesk, Level: notify.ActionNeeded,
		Key: "guest-message:" + a.res.String(), Title: "A guest wrote about booking " + a.code, ActionURL: "/messages", Now: now}); err != nil {
		return err
	}
	w.WriteHeader(http.StatusCreated)
	return nil
}

func (h *handler) mountChat(mux *http.ServeMux) {
	mux.HandleFunc("/api/manage/{token}/messages", httpx.Methods(map[string]http.HandlerFunc{http.MethodGet: h.withToken(h.guestMessages), http.MethodPost: h.withToken(h.guestSends)}))
	view := func(f http.HandlerFunc) http.HandlerFunc {
		return httpx.Wrap(access.Require(h.Deps, access.ReservationsView)(f))
	}
	edit := func(f http.HandlerFunc) http.HandlerFunc {
		return httpx.Wrap(access.Require(h.Deps, access.ReservationsEdit)(f))
	}
	mux.HandleFunc("/api/messages", httpx.Methods(map[string]http.HandlerFunc{http.MethodGet: view(h.threads)}))
	mux.HandleFunc("/api/reservations/{id}/messages", httpx.Methods(map[string]http.HandlerFunc{http.MethodGet: view(h.thread), http.MethodPost: edit(h.staffSends)}))
}

func (h *handler) threads(w http.ResponseWriter, r *http.Request) {
	a := access.ActorFrom(r.Context())
	type row struct {
		ReservationID uuid.UUID `json:"reservationId"`
		Code          string    `json:"code"`
		Guest         string    `json:"guest"`
		Last          string    `json:"last"`
		LastSender    string    `json:"lastSender"`
		LastAt        time.Time `json:"lastAt"`
		Unread        int       `json:"unread"`
	}
	out := []row{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select r.id, r.code, g.name, l.body, l.sender, l.created_at,
			(select count(*) from guest_message u where u.reservation_id = r.id and u.sender = 'guest' and u.read_at is null)
			from reservation r join guest g on g.id = r.guest_id
			join lateral (select body, sender, created_at from guest_message m where m.reservation_id = r.id order by created_at desc, id desc limit 1) l on true
			where r.property_id = $1 order by l.created_at desc limit 200`, a.PropertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x row
			if err := rows.Scan(&x.ReservationID, &x.Code, &x.Guest, &x.Last, &x.LastSender, &x.LastAt, &x.Unread); err != nil {
				return err
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func notFoundHere(w http.ResponseWriter) {
	httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
}

func (h *handler) thread(w http.ResponseWriter, r *http.Request) {
	a := access.ActorFrom(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil || a.PropertyID == uuid.Nil {
		notFoundHere(w)
		return
	}
	var out []messageOut
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(r.Context(), `select count(*) from reservation where id = $1 and property_id = $2`, id, a.PropertyID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return pgx.ErrNoRows
		}
		if _, err := tx.Exec(r.Context(), `update guest_message set read_at = $2 where reservation_id = $1 and sender = 'guest' and read_at is null`, id, h.Clock()); err != nil {
			return err
		}
		var e error
		out, e = loadMessages(r.Context(), tx, id)
		return e
	})
	switch {
	case err == pgx.ErrNoRows:
		notFoundHere(w)
	case err != nil:
		httpx.InternalError(w, err)
	default:
		httpx.JSON(w, http.StatusOK, out)
	}
}

func (h *handler) staffSends(w http.ResponseWriter, r *http.Request) {
	a := access.ActorFrom(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil || a.PropertyID == uuid.Nil {
		notFoundHere(w)
		return
	}
	body, ok := readMessage(w, r)
	if !ok {
		return
	}
	var email, code, hotel string
	err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `select g.email, r.code, p.name from reservation r join guest g on g.id = r.guest_id join property p on p.id = r.property_id where r.id = $1 and r.property_id = $2`,
			id, a.PropertyID).Scan(&email, &code, &hotel); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `insert into guest_message (id, organization_id, property_id, reservation_id, sender, body, created_at) values ($1, $2, $3, $4, 'staff', $5, $6)`,
			ids.New(), a.OrgID, a.PropertyID, id, body, h.Clock())
		return err
	})
	switch {
	case err == pgx.ErrNoRows:
		notFoundHere(w)
		return
	case err != nil:
		httpx.InternalError(w, err)
		return
	}
	if email != "" && h.Mail != nil {
		text := hotel + " replied to your message about booking " + code + ":\n\n" + body + "\n\nOpen your booking link to read the conversation and to reply. If the link has expired, ask for a new one on the booking page.\n"
		h.Spawn(func() {
			_ = h.Mail.Send(context.Background(), mail.Message{To: email, Subject: "A reply from " + hotel, Text: text})
		})
	}
	w.WriteHeader(http.StatusCreated)
}
