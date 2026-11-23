// Package risk scores the fraud risk of a guest booking with plain rules, keeps the score, and gives managers a queue
// of the bookings that need a look. A score never rejects a guest silently: a medium score asks the card for a 3-D
// Secure check, and a high score also puts the booking in the queue.
package risk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/access"
	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/httpx"
)

// Signals are what is known about a booking when it is made.
type Signals struct {
	Email               string
	Name                string
	Nights              int
	DaysToArrival       int
	Card                bool // the guest pays by card
	FromAddressLastHour int  // other bookings from the same network address in the last hour
	ForEmailLastDay     int  // other bookings for the same email in the last day
}

// Reason is one thing that raised the score.
type Reason struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// Result is the score, its level and the reasons for it.
type Result struct {
	Score   int      `json:"score"`
	Level   string   `json:"level"` // low, medium or high
	Reasons []Reason `json:"reasons"`
}

// disposable is a short list of throwaway email services. It is a hint, not a wall.
var disposable = map[string]bool{"mailinator.com": true, "guerrillamail.com": true, "10minutemail.com": true, "tempmail.com": true, "yopmail.com": true,
	"trashmail.com": true, "sharklasers.com": true, "throwawaymail.com": true, "getnada.com": true, "maildrop.cc": true}

// Score adds the weight of each signal. Each weight is small enough that no single weak signal blocks a real guest.
func Score(s Signals) Result {
	r := Result{Reasons: []Reason{}}
	add := func(n int, code, text string) {
		r.Score += n
		r.Reasons = append(r.Reasons, Reason{code, text})
	}
	if at := strings.LastIndex(s.Email, "@"); at >= 0 && disposable[strings.ToLower(s.Email[at+1:])] {
		add(40, "disposable_email", "The email address is from a throwaway email service.")
	}
	if s.FromAddressLastHour >= 3 {
		add(30, "many_from_address", "Three or more bookings came from the same network address in the last hour.")
	}
	if s.ForEmailLastDay >= 3 {
		add(25, "many_for_email", "Three or more bookings were made for this email in the last day.")
	}
	if s.Card && s.DaysToArrival <= 1 {
		add(10, "last_minute_card", "A card booking for a stay that starts today or tomorrow.")
	}
	if s.Nights >= 14 {
		add(10, "long_stay", "A stay of 14 nights or more.")
	}
	if !hasLetters(s.Name) {
		add(15, "odd_name", "The name has no letters or is a single character.")
	}
	switch {
	case r.Score >= 60:
		r.Level = "high"
	case r.Score >= 30:
		r.Level = "medium"
	default:
		r.Level = "low"
	}
	return r
}

func hasLetters(name string) bool {
	n := 0
	for _, c := range name {
		if unicode.IsLetter(c) {
			n++
		}
	}
	return n >= 2
}

// Hash makes the short fingerprint kept in place of an address or an email.
func Hash(s string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(s))))
	return hex.EncodeToString(sum[:16])
}

// Counts says how many bookings the same address made in the last hour and the same email in the last day.
func Counts(ctx context.Context, tx pgx.Tx, prop uuid.UUID, ipHash, emailHash string, now time.Time) (fromAddress, forEmail int, err error) {
	err = tx.QueryRow(ctx, `select count(*) filter (where ip_hash = $2 and created_at > $4::timestamptz - interval '1 hour'),
		count(*) filter (where email_hash = $3 and created_at > $4::timestamptz - interval '24 hours')
		from booking_risk where property_id = $1`, prop, ipHash, emailHash, now).Scan(&fromAddress, &forEmail)
	return
}

// Record keeps the score of a booking. A high score waits for staff in the queue, and the others are clear.
func Record(ctx context.Context, tx pgx.Tx, org, prop, reservation uuid.UUID, ipHash, emailHash string, r Result, now time.Time) error {
	status := "clear"
	if r.Level == "high" {
		status = "review"
	}
	reasons, err := json.Marshal(r.Reasons)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `insert into booking_risk (reservation_id, organization_id, property_id, score, level, reasons, status, ip_hash, email_hash, created_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`, reservation, org, prop, r.Score, r.Level, reasons, status, ipHash, emailHash, now)
	return err
}

type handler struct{ httpx.Deps }

// Mount adds the queue routes. Owners and managers, who approve refunds, also decide on flagged bookings.
func Mount(mux *http.ServeMux, d httpx.Deps) {
	h := &handler{d}
	wrap := func(method string, f http.HandlerFunc) http.HandlerFunc {
		return httpx.Methods(map[string]http.HandlerFunc{method: httpx.Wrap(access.Require(d, access.RefundsApprove)(f))})
	}
	mux.HandleFunc("/api/risk", wrap(http.MethodGet, h.list))
	mux.HandleFunc("/api/risk/{id}/approve", wrap(http.MethodPost, h.decide(true)))
	mux.HandleFunc("/api/risk/{id}/decline", wrap(http.MethodPost, h.decide(false)))
}

func scope(w http.ResponseWriter, r *http.Request) (access.Actor, bool) {
	a := access.ActorFrom(r.Context())
	if a.PropertyID == uuid.Nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "Choose a property first.")
		return a, false
	}
	return a, true
}

type queued struct {
	ReservationID uuid.UUID `json:"reservationId"`
	Code          string    `json:"code"`
	Guest         string    `json:"guest"`
	Score         int       `json:"score"`
	Level         string    `json:"level"`
	Reasons       []Reason  `json:"reasons"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"createdAt"`
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	a, ok := scope(w, r)
	if !ok {
		return
	}
	all := r.URL.Query().Get("all") == "1"
	out := []queued{}
	err := db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `select b.reservation_id, r.code, g.name, b.score, b.level, b.reasons, b.status, b.created_at
			from booking_risk b join reservation r on r.id = b.reservation_id join guest g on g.id = r.guest_id
			where b.property_id = $1 and ($2 or b.status = 'review') order by b.created_at desc limit 200`, a.PropertyID, all)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var q queued
			var reasons []byte
			if err := rows.Scan(&q.ReservationID, &q.Code, &q.Guest, &q.Score, &q.Level, &reasons, &q.Status, &q.CreatedAt); err != nil {
				return err
			}
			if err := json.Unmarshal(reasons, &q.Reasons); err != nil {
				return err
			}
			out = append(out, q)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

var errDecided = errors.New("risk: the booking was already decided")

func (h *handler) decide(approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, ok := scope(w, r)
		if !ok {
			return
		}
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
			return
		}
		now := h.Clock()
		err = db.WithOrg(r.Context(), h.Pool, a.OrgID, func(tx pgx.Tx) error {
			var status string
			if err := tx.QueryRow(r.Context(), `select status from booking_risk where reservation_id = $1 and property_id = $2 for update`, id, a.PropertyID).Scan(&status); err != nil {
				return err
			}
			if status != "review" {
				return errDecided
			}
			next := "declined"
			if approve {
				next = "approved"
			}
			if _, err := tx.Exec(r.Context(), `update booking_risk set status = $2, decided_by = $3, decided_at = $4 where reservation_id = $1`, id, next, a.UserID, now); err != nil {
				return err
			}
			if !approve {
				if err := booking.Cancel(r.Context(), tx, a.OrgID, a.PropertyID, id, now); err != nil {
					return err
				}
			}
			return audit.Record(r.Context(), tx, audit.Entry{Action: "booking_risk." + next, TargetType: "reservation", TargetID: id})
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			httpx.Error(w, http.StatusNotFound, "not_found", "There is nothing at this address.")
		case errors.Is(err, errDecided):
			httpx.Error(w, http.StatusConflict, "already_decided", "That booking was already decided.")
		case err != nil:
			httpx.InternalError(w, err)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}
}
