package guestbooking

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hotelpms/api/internal/audit"
	"hotelpms/api/internal/booking"
	"hotelpms/api/internal/db"
	"hotelpms/api/internal/folio"
	"hotelpms/api/internal/httpx"
	"hotelpms/api/internal/ids"
	"hotelpms/api/internal/mail"
)

// LinkLife is how long an emailed manage link works.
const LinkLife = time.Hour

var errRollbackPreview = errors.New("guestbooking: preview rolled back")

func hashOf(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// mount adds the manage routes next to the booking routes.
func (h *handler) mountManage(mux *http.ServeMux) {
	mux.HandleFunc("/api/public/{slug}/manage-link", httpx.Methods(map[string]http.HandlerFunc{http.MethodPost: h.requestLink}))
	mux.HandleFunc("/api/manage/{token}", httpx.Methods(map[string]http.HandlerFunc{http.MethodGet: h.withToken(h.view)}))
	mux.HandleFunc("/api/manage/{token}/resend", httpx.Methods(map[string]http.HandlerFunc{http.MethodPost: h.withToken(h.resend)}))
	mux.HandleFunc("/api/manage/{token}/cancel", httpx.Methods(map[string]http.HandlerFunc{http.MethodPost: h.cancel}))
}

// requestLink emails a manage link when the code and email match. It always answers 204, so it cannot be used to find out which codes exist.
func (h *handler) requestLink(w http.ResponseWriter, r *http.Request) {
	org, _, ok := h.resolve(w, r)
	if !ok || h.throttled(w, r, "link", 5, 10*time.Minute) {
		return
	}
	var in struct{ Code, Email string }
	if !readJSON(w, r, &in) {
		return
	}
	now := h.Clock()
	var to, text string
	err := db.WithOrg(r.Context(), h.Pool, org, func(tx pgx.Tx) error {
		var id uuid.UUID
		err := tx.QueryRow(r.Context(), `select r.id from reservation r join guest g on g.id = r.guest_id join property p on p.id = r.property_id
			where r.code = $1 and p.slug = $2 and g.email = $3`, strings.TrimSpace(in.Code), r.PathValue("slug"), booking.NormalizeEmail(in.Email)).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		raw := make([]byte, 24)
		if _, err := rand.Read(raw); err != nil {
			return err
		}
		secret := base64.RawURLEncoding.EncodeToString(raw)
		if _, err := tx.Exec(r.Context(), `insert into manage_token (token_hash, organization_id, reservation_id, expires_at) values ($1, $2, $3, $4)`, hashOf(secret), org, id, now.Add(LinkLife)); err != nil {
			return err
		}
		to = booking.NormalizeEmail(in.Email)
		text = "Open your booking: " + h.BaseURL + "/book/" + r.PathValue("slug") + "/manage?token=" + org.String() + "." + secret + "\nThe link works for one hour."
		return nil
	})
	if err != nil {
		httpx.InternalError(w, err)
		return
	}
	if to != "" && h.Mail != nil {
		h.Spawn(func() {
			_ = h.Mail.Send(context.Background(), mail.Message{To: to, Subject: "Your booking link", Text: text})
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

type booked struct {
	org, prop, res uuid.UUID
	code, email    string
}

// lookup finds the booking a valid, unexpired token opens, inside the organization the token names.
func (h *handler) lookup(ctx context.Context, tx pgx.Tx, org uuid.UUID, secret string) (booked, error) {
	a := booked{org: org}
	err := tx.QueryRow(ctx, `select r.id, r.property_id, r.code, g.email from manage_token t
		join reservation r on r.id = t.reservation_id join guest g on g.id = r.guest_id
		where t.token_hash = $1 and t.expires_at > $2`, hashOf(secret), h.Clock()).Scan(&a.res, &a.prop, &a.code, &a.email)
	return a, err
}

func splitToken(token string) (uuid.UUID, string, bool) {
	orgText, secret, found := strings.Cut(token, ".")
	org, err := uuid.Parse(orgText)
	return org, secret, found && err == nil
}

func notValid(w http.ResponseWriter) {
	httpx.Error(w, http.StatusNotFound, "not_found", "This link is not valid any more. Ask for a new one.")
}

// withToken runs f for the booking a valid token opens; anything else is a 404.
func (h *handler) withToken(f func(http.ResponseWriter, *http.Request, pgx.Tx, booked) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.throttled(w, r, "manage", 60, time.Minute) {
			return
		}
		org, secret, ok := splitToken(r.PathValue("token"))
		if !ok {
			notValid(w)
			return
		}
		err := db.WithOrg(r.Context(), h.Pool, org, func(tx pgx.Tx) error {
			a, err := h.lookup(r.Context(), tx, org, secret)
			if err != nil {
				return err
			}
			return f(w, r, tx, a)
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			notValid(w)
		case errors.Is(err, errRollbackPreview), err == nil:
		default:
			httpx.InternalError(w, err)
		}
	}
}

// cardShare is part of a card payment that can still go back to the guest.
type cardShare struct {
	payment uuid.UUID
	ref     string
	amount  int64
}

// settle cancels the booking inside tx and reads what follows: how much goes back to the card and what the hotel keeps.
// Callers either keep the cancellation or roll it back, which is how a preview can never differ from the real thing.
func settle(ctx context.Context, tx pgx.Tx, a booked, now time.Time) (refund, penalty int64, shares []cardShare, err error) {
	if err = booking.Cancel(ctx, tx, a.org, a.prop, a.res, now); err != nil {
		return
	}
	var balance int64
	if balance, _, err = folio.Balance(ctx, tx, a.org, a.prop, a.res, now); err != nil {
		return
	}
	if balance >= 0 {
		return 0, balance, nil, nil
	}
	owed := -balance
	rows, err := tx.Query(ctx, `select p.id, p.provider_ref, p.amount - coalesce((select sum(r.amount) from refund r where r.payment_id = p.id and r.status in ('done', 'pending')), 0)
		from payment p join folio f on f.id = p.folio_id
		where f.reservation_id = $1 and p.method = 'card_online' and p.provider_ref is not null order by p.created_at, p.id`, a.res)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() && owed > 0 {
		var s cardShare
		if err = rows.Scan(&s.payment, &s.ref, &s.amount); err != nil {
			return
		}
		if s.amount <= 0 {
			continue
		}
		if s.amount > owed {
			s.amount = owed
		}
		owed -= s.amount
		refund += s.amount
		shares = append(shares, s)
	}
	err = rows.Err()
	return
}

func (h *handler) view(w http.ResponseWriter, r *http.Request, tx pgx.Tx, a booked) error {
	ctx := r.Context()
	now := h.Clock()
	out := struct {
		Code        string    `json:"code"`
		Status      string    `json:"status"`
		From        string    `json:"from"`
		To          string    `json:"to"`
		Currency    string    `json:"currency"`
		Paid        int64     `json:"paid"`
		Cancellable bool      `json:"cancellable"`
		Refund      int64     `json:"refund"`
		Penalty     int64     `json:"penalty"`
		HoldUntil   time.Time `json:"holdUntil,omitzero"`
		Hotel       string    `json:"hotel"`
		Address     string    `json:"address"`
	}{Code: a.code}
	var hold *time.Time
	if err := tx.QueryRow(ctx, `select r.status, r.currency, r.hold_until, p.name, p.address,
			(select to_char(min(lower(stay)), 'YYYY-MM-DD') from reservation_room where reservation_id = r.id),
			(select to_char(max(upper(stay)), 'YYYY-MM-DD') from reservation_room where reservation_id = r.id),
			(select coalesce(sum(amount), 0) from payment where folio_id in (select id from folio where reservation_id = r.id))
			- (select coalesce(sum(f.amount), 0) from refund f join payment py on py.id = f.payment_id join folio fo on fo.id = py.folio_id where fo.reservation_id = r.id and f.status = 'done')
		from reservation r join property p on p.id = r.property_id where r.id = $1`, a.res).Scan(&out.Status, &out.Currency, &hold, &out.Hotel, &out.Address, &out.From, &out.To, &out.Paid); err != nil {
		return err
	}
	if hold != nil {
		out.HoldUntil = *hold
	}
	if out.Status == "pending" || out.Status == "confirmed" {
		out.Cancellable = true
		var err error
		if out.Refund, out.Penalty, _, err = settle(ctx, tx, a, now); err != nil {
			return err
		}
		// Nothing above may stay: this was only to read the figures.
		httpx.JSON(w, http.StatusOK, out)
		return errRollbackPreview
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

func (h *handler) resend(w http.ResponseWriter, r *http.Request, tx pgx.Tx, a booked) error {
	var status string
	var hold *time.Time
	var cur string
	if err := tx.QueryRow(r.Context(), `select status, hold_until, currency from reservation where id = $1`, a.res).Scan(&status, &hold, &cur); err != nil {
		return err
	}
	m := madeOut{Code: a.code, Status: status, Currency: cur}
	if hold != nil {
		m.HoldUntil = *hold
	}
	due, _, err := folio.Balance(r.Context(), tx, a.org, a.prop, a.res, h.Clock())
	if err != nil {
		return err
	}
	m.Total = due
	defer h.mailGuest(a.email, m, "")
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *handler) cancel(w http.ResponseWriter, r *http.Request) {
	if h.throttled(w, r, "manage", 60, time.Minute) {
		return
	}
	org, secret, ok := splitToken(r.PathValue("token"))
	if !ok {
		notValid(w)
		return
	}
	ctx := r.Context()
	now := h.Clock()
	type waiting struct {
		id uuid.UUID
		cardShare
	}
	var refund, penalty int64
	var todo []waiting
	// Step 1: cancel and write the waiting refunds in one transaction. A provider failure later cannot undo this.
	err := db.WithOrg(ctx, h.Pool, org, func(tx pgx.Tx) error {
		a, err := h.lookup(ctx, tx, org, secret)
		if err != nil {
			return err
		}
		var shares []cardShare
		if refund, penalty, shares, err = settle(ctx, tx, a, now); err != nil {
			return err
		}
		for _, s := range shares {
			id := ids.New()
			if _, err := tx.Exec(ctx, `insert into refund (id, organization_id, payment_id, amount, reason, status, created_at) values ($1, $2, $3, $4, 'Guest cancelled within the rate plan policy', 'pending', $5)`, id, org, s.payment, s.amount, now); err != nil {
				return err
			}
			todo = append(todo, waiting{id, s})
		}
		return audit.RecordSystem(ctx, tx, org, "guest-site", audit.Entry{Action: "reservation.guest_cancelled", TargetType: "reservation", TargetID: a.res, TargetLabel: a.code, After: map[string]any{"refund": refund, "penalty": penalty}})
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		notValid(w)
		return
	case errors.Is(err, booking.ErrCannotCancel), errors.Is(err, booking.ErrAlreadyCancelled):
		httpx.Error(w, http.StatusConflict, "cannot_cancel", "This booking can no longer be cancelled here. Contact the hotel.")
		return
	case err != nil:
		httpx.InternalError(w, err)
		return
	}
	// Step 2: ask the provider. What it refuses stays a waiting refund for staff.
	stuck := false
	for _, p := range todo {
		rf, err := h.card.RefundPayment(ctx, p.ref, p.amount, "refund-"+p.id.String())
		if err != nil {
			stuck = true
			continue
		}
		if err := db.WithOrg(ctx, h.Pool, org, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `update refund set status = 'done', provider_ref = $2, decided_at = $3 where id = $1`, p.id, rf.ID, now); err != nil {
				return err
			}
			return folio.RefreshPayment(ctx, tx, p.payment)
		}); err != nil {
			stuck = true
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"status": "cancelled", "refund": refund, "penalty": penalty, "refundPending": stuck})
}
