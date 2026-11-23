// Package guestcare holds two small promises to guests. A guest whose booking is likely to be cancelled is asked to
// reconfirm, and a guest on the waiting list is told when a room frees up. Nothing here cancels a booking or gives a room
// away: staff decide what to do when a guest does not answer.
package guestcare

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	netmail "net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/cancelrisk"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/ids"
	"hotelpms/api/internal/mail"
	"hotelpms/api/internal/ratelimit"
)

// reconfirmDays is how many days before arrival a guest at high risk is asked to reconfirm.
const reconfirmDays = 3

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

type property struct{ org, prop uuid.UUID }

func properties(ctx context.Context, pool *pgxpool.Pool) ([]property, error) {
	var out []property
	err := db.WithoutOrg(ctx, pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select organization_id, property_id from all_properties()`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p property
			if err := rows.Scan(&p.org, &p.prop); err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// RunReconfirm asks each guest at high cancel risk, whose stay starts within three days, to reconfirm. A guest is asked
// once. It says how many guests it asked.
func RunReconfirm(ctx context.Context, pool *pgxpool.Pool, sender mail.Sender, baseURL string, now time.Time) (int, error) {
	props, err := properties(ctx, pool)
	if err != nil {
		return 0, err
	}
	asked := 0
	var failures []error
	for _, p := range props {
		type ask struct {
			res                               uuid.UUID
			email, name, code, arrival, token string
		}
		var todo []ask
		err := db.WithOrg(ctx, pool, p.org, func(tx pgx.Tx) error {
			today, _, err := booking.HotelToday(ctx, tx, p.org, p.prop, now)
			if err != nil {
				return err
			}
			items, err := cancelrisk.Upcoming(ctx, tx, p.prop, today, today.AddDate(0, 0, reconfirmDays))
			if err != nil {
				return err
			}
			for _, it := range items {
				if it.Level != "high" {
					continue
				}
				var email, name string
				if err := tx.QueryRow(ctx, `select g.email, g.name from reservation r join guest g on g.id = r.guest_id where r.id = $1`, it.ReservationID).Scan(&email, &name); err != nil {
					return err
				}
				if email == "" {
					continue
				}
				raw := make([]byte, 32)
				if _, err := rand.Read(raw); err != nil {
					return err
				}
				token := hex.EncodeToString(raw)
				tag, err := tx.Exec(ctx, `insert into reconfirm (reservation_id, organization_id, property_id, token_hash, sent_at) values ($1, $2, $3, $4, $5) on conflict do nothing`,
					it.ReservationID, p.org, p.prop, hashToken(token), now)
				if err != nil {
					return err
				}
				if tag.RowsAffected() == 1 {
					todo = append(todo, ask{it.ReservationID, email, name, it.Code, it.Arrival, token})
				}
			}
			return nil
		})
		if err != nil {
			failures = append(failures, fmt.Errorf("property %s: %w", p.prop, err))
			continue
		}
		for _, a := range todo {
			text := fmt.Sprintf("Hello %s,\n\nWe look forward to your stay from %s (booking %s).\nPlease confirm that you are coming:\n%s/reconfirm/%s/%s\n\n"+
				"If your plans changed, please cancel from your booking link so that another guest can have the room.\n", a.name, a.arrival, a.code, strings.TrimRight(baseURL, "/"), p.org, a.token)
			if err := sender.Send(ctx, mail.Message{To: a.email, Subject: "Please confirm your stay, booking " + a.code, Text: text}); err != nil {
				// Forget the request, so the next run tries again.
				_ = db.WithOrg(ctx, pool, p.org, func(tx pgx.Tx) error {
					_, e := tx.Exec(ctx, `delete from reconfirm where reservation_id = $1`, a.res)
					return e
				})
				failures = append(failures, err)
				continue
			}
			asked++
		}
	}
	return asked, errors.Join(failures...)
}

// NotifyWaitlist tells each waiting guest once when some room type is free for every night they asked for.
func NotifyWaitlist(ctx context.Context, pool *pgxpool.Pool, sender mail.Sender, baseURL string, now time.Time) (int, error) {
	props, err := properties(ctx, pool)
	if err != nil {
		return 0, err
	}
	told := 0
	var failures []error
	for _, p := range props {
		type tell struct {
			id                          uuid.UUID
			email, name, arrive, depart string
		}
		var todo []tell
		var slug string
		err := db.WithOrg(ctx, pool, p.org, func(tx pgx.Tx) error {
			today, _, err := booking.HotelToday(ctx, tx, p.org, p.prop, now)
			if err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `select slug from property where id = $1`, p.prop).Scan(&slug); err != nil {
				return err
			}
			rows, err := tx.Query(ctx, `select w.id, w.email, w.name, to_char(w.arrive, 'YYYY-MM-DD'), to_char(w.depart, 'YYYY-MM-DD') from waitlist w
				where w.property_id = $1 and w.notified_at is null and w.arrive >= $2::date
				and exists (select 1 from room_type t where t.property_id = $1 and not exists (
					select 1 from generate_series(w.arrive, w.depart - 1, interval '1 day') d
					where coalesce((select greatest(i.total - i.sold - i.blocked, 0) from inventory i where i.room_type_id = t.id and i.date = d::date), 0) < 1))
				order by w.created_at`, p.prop, today)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var x tell
				if err := rows.Scan(&x.id, &x.email, &x.name, &x.arrive, &x.depart); err != nil {
					return err
				}
				todo = append(todo, x)
			}
			return rows.Err()
		})
		if err != nil {
			failures = append(failures, fmt.Errorf("property %s: %w", p.prop, err))
			continue
		}
		for _, x := range todo {
			text := fmt.Sprintf("Hello %s,\n\nA room is free from %s to %s.\nRooms go to the first guest who books, so please book soon:\n%s/book/%s\n", x.name, x.arrive, x.depart, strings.TrimRight(baseURL, "/"), slug)
			if err := sender.Send(ctx, mail.Message{To: x.email, Subject: "A room is free for your dates", Text: text}); err != nil {
				failures = append(failures, err)
				continue
			}
			if err := db.WithOrg(ctx, pool, p.org, func(tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `update waitlist set notified_at = $2 where id = $1`, x.id, now)
				return e
			}); err != nil {
				failures = append(failures, err)
				continue
			}
			told++
		}
	}
	return told, errors.Join(failures...)
}

// RunSweeper runs both jobs on a timer until the context ends.
func RunSweeper(ctx context.Context, pool *pgxpool.Pool, sender mail.Sender, baseURL string, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, _ = RunReconfirm(ctx, pool, sender, baseURL, time.Now())
			_, _ = NotifyWaitlist(ctx, pool, sender, baseURL, time.Now())
		}
	}
}

type handler struct{ httpx.Deps }

// Mount adds the public reconfirm and waiting list routes and the staff list.
func Mount(mux *http.ServeMux, d httpx.Deps) {
	h := &handler{d}
	mux.HandleFunc("/api/reconfirm/{org}/{token}", httpx.Methods(map[string]http.HandlerFunc{http.MethodPost: h.confirm}))
	mux.HandleFunc("/api/public/{slug}/waitlist", httpx.Methods(map[string]http.HandlerFunc{http.MethodPost: h.join}))
	mux.HandleFunc("/api/waitlist", httpx.Methods(map[string]http.HandlerFunc{http.MethodGet: httpx.Wrap(access.Require(d, access.ReservationsView)(http.HandlerFunc(h.list)))}))
}

func notFound(w http.ResponseWriter) {
	httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
}

func (h *handler) throttled(w http.ResponseWriter, r *http.Request, what string, max int) bool {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if ratelimit.Allow(r.Context(), h.Pool, what+":"+ip, h.Clock(), max, 10*time.Minute) {
		return false
	}
	httpx.Error(w, http.StatusTooManyRequests, "slow_down", "Too many requests. Wait a few minutes and try again.")
	return true
}

func (h *handler) confirm(w http.ResponseWriter, r *http.Request) {
	if h.throttled(w, r, "reconfirm", 30) {
		return
	}
	org, err := uuid.Parse(r.PathValue("org"))
	token := r.PathValue("token")
	if _, e := hex.DecodeString(token); err != nil || e != nil || len(token) != 64 {
		notFound(w)
		return
	}
	now := h.Clock()
	err = db.WithOrg(r.Context(), h.Pool, org, func(tx pgx.Tx) error {
		tag, e := tx.Exec(r.Context(), `update reconfirm set confirmed_at = coalesce(confirmed_at, $2) where token_hash = $1`, hashToken(token), now)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return nil
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		notFound(w)
	case err != nil:
		httpx.InternalError(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

type joinIn struct {
	Name   string `json:"name"`
	Email  string `json:"email"`
	Arrive string `json:"arrive"`
	Depart string `json:"depart"`
	Adults int    `json:"adults"`
	Locale string `json:"locale"`
}

func (h *handler) join(w http.ResponseWriter, r *http.Request) {
	if h.throttled(w, r, "waitlist", 10) {
		return
	}
	var in joinIn
	d := json.NewDecoder(io.LimitReader(r.Body, 8<<10))
	d.DisallowUnknownFields()
	bad := func() {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Check your name, email and the dates, then try again.")
	}
	if err := d.Decode(&in); err != nil {
		bad()
		return
	}
	in.Name, in.Email = strings.TrimSpace(in.Name), strings.ToLower(strings.TrimSpace(in.Email))
	a, e1 := time.Parse("2006-01-02", in.Arrive)
	b, e2 := time.Parse("2006-01-02", in.Depart)
	addr, e3 := netmail.ParseAddress(in.Email)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 200 || e1 != nil || e2 != nil || !b.After(a) || b.Sub(a) > 60*24*time.Hour ||
		e3 != nil || addr.Address != in.Email || len(in.Email) > 200 || in.Adults < 1 || in.Adults > 20 || len(in.Locale) > 10 {
		bad()
		return
	}
	if in.Locale == "" {
		in.Locale = "en"
	}
	var org, prop uuid.UUID
	if err := db.WithoutOrg(r.Context(), h.Pool, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `select organization_id, property_id from property_by_slug($1)`, r.PathValue("slug")).Scan(&org, &prop)
	}); err != nil {
		notFound(w)
		return
	}
	now := h.Clock()
	err := db.WithOrg(r.Context(), h.Pool, org, func(tx pgx.Tx) error {
		today, _, err := booking.HotelToday(r.Context(), tx, org, prop, now)
		if err != nil {
			return err
		}
		if a.Before(today) {
			return errPast
		}
		_, err = tx.Exec(r.Context(), `insert into waitlist (id, organization_id, property_id, name, email, locale, arrive, depart, adults, created_at) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			on conflict (property_id, email, arrive, depart) do nothing`, ids.New(), org, prop, in.Name, in.Email, in.Locale, in.Arrive, in.Depart, in.Adults, now)
		return err
	})
	switch {
	case errors.Is(err, errPast):
		bad()
	case err != nil:
		httpx.InternalError(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

var errPast = errors.New("guestcare: the dates are in the past")

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	a := access.ActorFrom(r.Context())
	if a.PropertyID == uuid.Nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Choose a property first.")
		return
	}
	type row struct {
		Name      string    `json:"name"`
		Email     string    `json:"email"`
		Arrive    string    `json:"arrive"`
		Depart    string    `json:"depart"`
		Adults    int       `json:"adults"`
		CreatedAt time.Time `json:"createdAt"`
		Notified  bool      `json:"notified"`
	}
	out := []row{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select name, email, to_char(arrive, 'YYYY-MM-DD'), to_char(depart, 'YYYY-MM-DD'), adults, created_at, notified_at is not null
			from waitlist where property_id = $1 order by created_at desc limit 200`, a.PropertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x row
			if err := rows.Scan(&x.Name, &x.Email, &x.Arrive, &x.Depart, &x.Adults, &x.CreatedAt, &x.Notified); err != nil {
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
